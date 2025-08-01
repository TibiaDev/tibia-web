package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"sync"
	"time"
)

type TSession struct {
	SessionID []byte
	IPAddress string
	Expires   time.Time
	AccountID int
}

// IMPORTANT(fusion): Ideally you'd save sessions in a database to reduce memory
// usage and to make them persistent with server restarts. In reality, we should
// have a low amount of sessions and a high server uptime, making the memory usage
// here minimal. We can always turn this into a LRU cache with a set maximum number
// of sessions.

var (
	g_SessionsMutex sync.Mutex
	g_Sessions      []TSession
)

func GenerateSessionID() []byte {
	var SessionID [32]byte
	_, Err := rand.Read(SessionID[:])
	if Err != nil {
		g_LogErr.Printf("Failed to generate session id: %v", Err)
		return nil
	}

	return SessionID[:]
}

func GetRequestSessionID(Request *http.Request) []byte {
	Cookie, Err := Request.Cookie("GOSESSID")
	if Err != nil {
		return nil
	}

	SessionID, Err := hex.DecodeString(Cookie.Value)
	if Err != nil {
		g_LogErr.Printf("Failed to decode session id: %v", Err)
		return nil
	}

	if len(SessionID) != 32 {
		g_LogErr.Printf("Invalid session id size %v (expected 32)", len(SessionID))
		return nil
	}

	return SessionID
}

func SessionLookup(SessionID []byte, IPAddress string) int {
	AccountID := 0
	if SessionID != nil && IPAddress != "" {
		g_SessionsMutex.Lock()
		defer g_SessionsMutex.Unlock()
		for Index := 0; Index < len(g_Sessions); Index += 1 {
			Session := &g_Sessions[Index]

			if time.Until(Session.Expires) <= 0 {
				g_Sessions = SwapAndPop(g_Sessions, Index)
				Index -= 1
				continue
			}

			if bytes.Equal(Session.SessionID, SessionID) && Session.IPAddress == IPAddress {
				AccountID = Session.AccountID
				break
			}
		}
	}
	return AccountID
}

func SessionStart(Context *THttpRequestContext, AccountID int) {
	if AccountID <= 0 {
		g_LogErr.Printf("Trying to start session with invalid account id %v", AccountID)
		return
	}

	SessionID := make([]byte, 32)
	if _, Err := rand.Read(SessionID); Err != nil {
		g_LogErr.Printf("Failed to generate session id: %v", Err)
		return
	}

	Context.SessionID = SessionID
	Context.AccountID = AccountID
	Expires := time.Now().Add(time.Hour)
	http.SetCookie(Context.Writer, &http.Cookie{
		Name:     "GOSESSID",
		Value:    hex.EncodeToString(SessionID),
		Path:     "/",
		Expires:  Expires,
		Secure:   false, // TODO(fusion): Enable this when HTTPS is enabled (?).
		HttpOnly: true,
	})

	g_SessionsMutex.Lock()
	defer g_SessionsMutex.Unlock()
	g_Sessions = append(g_Sessions,
		TSession{
			SessionID: SessionID,
			IPAddress: Context.IPAddress,
			Expires:   Expires,
			AccountID: AccountID,
		})
}

func SessionEnd(Context *THttpRequestContext) {
	if Context.SessionID == nil {
		return
	}

	http.SetCookie(Context.Writer, &http.Cookie{
		Name:    "GOSESSID",
		Value:   "",
		Path:    "/",
		Expires: time.Unix(0, 0),
	})

	g_SessionsMutex.Lock()
	defer g_SessionsMutex.Unlock()
	for Index := 0; Index < len(g_Sessions); Index += 1 {
		Session := &g_Sessions[Index]
		if bytes.Equal(Session.SessionID, Context.SessionID) && Session.IPAddress == Context.IPAddress {
			g_Sessions[Index] = g_Sessions[len(g_Sessions)-1]
			g_Sessions[len(g_Sessions)-1] = TSession{}
			g_Sessions = g_Sessions[:len(g_Sessions)-1]
			break
		}
	}
}
