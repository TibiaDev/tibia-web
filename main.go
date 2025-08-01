package main

import (
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"
)

type (
	THttpHandler = func(Context *THttpRequestContext)

	THttpRoute struct {
		Method      string
		Prefix      string
		AllowParams bool
		Handler     THttpHandler
	}

	THttpRouter struct {
		Routes   []THttpRoute
		NotFound THttpHandler
	}

	THttpRequestContext struct {
		Request   *http.Request
		Writer    http.ResponseWriter
		Prefix    string
		Params    []string
		IPAddress string
		SessionID []byte
		AccountID int
	}
)

var (
	// HTTP/HTTPS Config
	g_HttpPort      int    = 80
	g_HttpsPort     int    = 443
	g_HttpsCertFile string = ""
	g_HttpsKeyFile  string = ""

	// SMTP Config
	g_SmtpHost     string = "smtp.domain.com"
	g_SmtpPort     int    = 587
	g_SmtpUser     string = "username"
	g_SmtpPassword string = ""
	g_SmtpSender   string = "support@domain.com"

	// Query Manager Config
	g_QueryManagerHost     string = "localhost"
	g_QueryManagerPort     int    = 7174
	g_QueryManagerPassword string = ""

	// Query Manager Cache Config
	g_MaxCachedAccounts               = 4096
	g_MaxCachedCharacters             = 4096
	g_CharacterRefreshInterval        = 15 * time.Minute
	g_WorldsRefreshInterval           = 15 * time.Minute
	g_OnlineCharactersRefreshInterval = 15 * time.Minute
	g_KillStatisticsRefreshInterval   = 30 * time.Minute

	// Loggers
	g_Log     = log.New(os.Stderr, "INFO ", log.Ldate|log.Ltime|log.Lmsgprefix)
	g_LogWarn = log.New(os.Stderr, "WARN ", log.Ldate|log.Ltime|log.Lshortfile|log.Lmsgprefix)
	g_LogErr  = log.New(os.Stderr, "ERR  ", log.Ldate|log.Ltime|log.Lshortfile|log.Lmsgprefix)
)

func WebKVCallback(Key string, Value string) {
	if strings.EqualFold(Key, "HttpPort") {
		g_HttpPort = ParseInteger(Value)
	} else if strings.EqualFold(Key, "HttpsPort") {
		g_HttpsPort = ParseInteger(Value)
	} else if strings.EqualFold(Key, "HttpsCertFile") {
		g_HttpsCertFile = ParseString(Value)
	} else if strings.EqualFold(Key, "HttpsKeyFile") {
		g_HttpsKeyFile = ParseString(Value)
	} else if strings.EqualFold(Key, "SmtpHost") {
		g_SmtpHost = ParseString(Value)
	} else if strings.EqualFold(Key, "SmtpPort") {
		g_SmtpPort = ParseInteger(Value)
	} else if strings.EqualFold(Key, "SmtpUser") {
		g_SmtpUser = ParseString(Value)
	} else if strings.EqualFold(Key, "SmtpPassword") {
		g_SmtpPassword = ParseString(Value)
	} else if strings.EqualFold(Key, "SmtpSender") {
		g_SmtpSender = ParseString(Value)
	} else if strings.EqualFold(Key, "QueryManagerHost") {
		g_QueryManagerHost = ParseString(Value)
	} else if strings.EqualFold(Key, "QueryManagerPort") {
		g_QueryManagerPort = ParseInteger(Value)
	} else if strings.EqualFold(Key, "QueryManagerPassword") {
		g_QueryManagerPassword = ParseString(Value)
	} else if strings.EqualFold(Key, "CharacterRefreshInterval") {
		g_CharacterRefreshInterval = ParseDuration(Value)
	} else if strings.EqualFold(Key, "WorldsRefreshInterval") {
		g_WorldsRefreshInterval = ParseDuration(Value)
	} else if strings.EqualFold(Key, "OnlineCharactersRefreshInterval") {
		g_OnlineCharactersRefreshInterval = ParseDuration(Value)
	} else if strings.EqualFold(Key, "KillStatisticsRefreshInterval") {
		g_KillStatisticsRefreshInterval = ParseDuration(Value)
	} else if strings.EqualFold(Key, "MaxCachedAccounts") {
		g_MaxCachedAccounts = ParseInteger(Value)
	} else if strings.EqualFold(Key, "MaxCachedCharacters") {
		g_MaxCachedCharacters = ParseInteger(Value)
	} else {
		g_LogWarn.Printf("Unknown config \"%v\"", Key)
	}
}

func (Route *THttpRoute) LessThan(Method string, Prefix string, AllowParams bool) bool {
	if Route.Method != Method {
		return Route.Method < Method
	} else if Route.Prefix != Prefix {
		return Route.Prefix < Prefix
	} else {
		// NOTE(fusion): Use `false < true` convention.
		return !Route.AllowParams && AllowParams
	}
}

func (Router *THttpRouter) Add(Method string, Prefix string, Handler THttpHandler) {
	AllowParams := false
	if Prefix != "/" && Prefix[len(Prefix)-1] == '/' {
		AllowParams = true
		Prefix = Prefix[:len(Prefix)-1]
	}

	Index := 0
	for ; Index < len(Router.Routes); Index += 1 {
		if !Router.Routes[Index].LessThan(Method, Prefix, AllowParams) {
			break
		}
	}

	if Index < len(Router.Routes) &&
		Router.Routes[Index].Method == Method &&
		Router.Routes[Index].Prefix == Prefix &&
		Router.Routes[Index].AllowParams == AllowParams {
		if AllowParams {
			g_LogErr.Printf("Discarding duplicate route \"%v %v[/params]\"",
				Method, Prefix)
		} else {
			g_LogErr.Printf("Discarding duplicate route \"%v %v\"",
				Method, Prefix)
		}
		return
	}

	Router.Routes = slices.Insert(Router.Routes, Index,
		THttpRoute{
			Method:      Method,
			Prefix:      Prefix,
			AllowParams: AllowParams,
			Handler:     Handler,
		})
}

func GetRequestIPAddress(Request *http.Request) string {
	// NOTE(fusion): `Request.RemoteAddr` should to be in the exact format
	// expected by `net.SplitHostPort` so I expect this to NEVER fail.
	IPAddress, _, Err := net.SplitHostPort(Request.RemoteAddr)
	if Err != nil {
		g_LogErr.Printf("net.SplitHostPort(\"%v\") failed: %v", Request.RemoteAddr, Err)
		return ""
	}

	return IPAddress
}

func (Router *THttpRouter) ServeHTTP(Writer http.ResponseWriter, Request *http.Request) {
	Path := Request.URL.Path
	if Path == "" {
		Path = "/"
	}

	IPAddress := GetRequestIPAddress(Request)
	if IPAddress == "" {
		http.Error(Writer, "", http.StatusBadRequest)
		return
	}

	SessionID := GetRequestSessionID(Request)
	Context := THttpRequestContext{
		Request:   Request,
		Writer:    Writer,
		Prefix:    Path,
		Params:    nil,
		IPAddress: IPAddress,
		SessionID: SessionID,
		AccountID: SessionLookup(SessionID, IPAddress),
	}

	for Index := len(Router.Routes) - 1; Index >= 0; Index -= 1 {
		Route := &Router.Routes[Index]
		if Route.Method != "" && Route.Method != Request.Method {
			continue
		}

		Suffix, Found := strings.CutPrefix(Path, Route.Prefix)
		if Found && (Suffix == "" || Suffix[0] == '/') {
			Params := SplitDiscardEmpty(Suffix, "/")
			if Route.AllowParams || len(Params) == 0 {
				Context.Prefix = Route.Prefix
				Context.Params = Params
				Route.Handler(&Context)
				return
			}
		}
	}

	Router.NotFound(&Context)
}

func Redirect(Context *THttpRequestContext, Path string) {
	Context.Writer.Header().Set("Location", Path)
	Context.Writer.WriteHeader(http.StatusTemporaryRedirect)
}

func RequestError(Context *THttpRequestContext, Status int) {
	g_LogErr.Printf("Failed to serve request \"%v %v\" to \"%v\": (%v) %v",
		Context.Request.Method, Context.Request.URL.Path, Context.Request.RemoteAddr,
		Status, http.StatusText(Status))
	RenderRequestError(Context, Status)
}

func BadRequest(Context *THttpRequestContext) {
	RequestError(Context, http.StatusBadRequest)
}

func Forbidden(Context *THttpRequestContext) {
	RequestError(Context, http.StatusForbidden)
}

func NotFound(Context *THttpRequestContext) {
	RequestError(Context, http.StatusNotFound)
}

func InternalError(Context *THttpRequestContext) {
	RequestError(Context, http.StatusInternalServerError)
}

func ResourceError(Context *THttpRequestContext, Status int) {
	// IMPORTANT(fusion): This is used for resource errors in which case we
	// don't want to render any HTML to avoid pointless traffic. `http.Error`
	// should send a minimal response with the appropriate status code.
	g_LogErr.Printf("Failed to fetch resource \"%v %v\" to \"%v\": (%v) %v",
		Context.Request.Method, Context.Request.URL.Path, Context.Request.RemoteAddr,
		Status, http.StatusText(Status))
	http.Error(Context.Writer, "", Status)
}

func HandleResource(Context *THttpRequestContext) {
	if len(Context.Params) == 0 {
		ResourceError(Context, http.StatusNotFound)
		return
	}

	FileName := path.Join(Context.Params...)
	File, Err := os.OpenInRoot("./res", FileName)
	if Err != nil {
		g_LogErr.Printf("Failed to open file (%v): %v", FileName, Err)
		ResourceError(Context, http.StatusNotFound)
		return
	}
	defer File.Close()

	Stat, Err := File.Stat()
	if Err != nil {
		g_LogErr.Printf("Failed to retrieve file description (%v): %v", FileName, Err)
		ResourceError(Context, http.StatusInternalServerError)
		return
	}

	// NOTE(fusion): File headers.
	switch path.Ext(FileName) {
	case ".css":
		Context.Writer.Header().Set("Content-Type", "text/css")
	case ".jpg", ".jpeg":
		Context.Writer.Header().Set("Content-Type", "image/jpeg")
	case ".js":
		Context.Writer.Header().Set("Content-Type", "text/javascript")
	case ".png":
		Context.Writer.Header().Set("Content-Type", "image/png")
	default:
		Context.Writer.Header().Set("Content-Disposition",
			fmt.Sprintf("attachment; filename=\"%v\"", FileName))
		Context.Writer.Header().Set("Content-Type", "application/octet-stream")
	}
	Context.Writer.Header().Set("Content-Length", strconv.FormatInt(Stat.Size(), 10))
	Context.Writer.Header().Set("Date", time.Now().UTC().Format(http.TimeFormat))
	Context.Writer.Header().Set("Last-Modified", Stat.ModTime().UTC().Format(http.TimeFormat))

	// NOTE(fusion): File contents.
	TotalRead := 0
	TotalWritten := 0
	for {
		var Buffer [1024 * 1024]byte
		BytesRead, Err := File.Read(Buffer[:])
		if Err != nil && Err != io.EOF {
			g_LogErr.Printf("Failed to read resource (%v:%v): %v", FileName, TotalRead, Err)
			return
		}

		if BytesRead == 0 {
			return
		}

		BytesWritten, Err := Context.Writer.Write(Buffer[:BytesRead])
		if Err != nil || BytesWritten != BytesRead {
			g_LogErr.Printf("Failed to write resource (%v:%v): %v", FileName, TotalWritten, Err)
			return
		}

		TotalRead += BytesRead
		TotalWritten += BytesWritten
	}
}

func HandleFavicon(Context *THttpRequestContext) {
	if len(Context.Params) != 0 {
		ResourceError(Context, http.StatusNotFound)
		return
	}

	Context.Params = []string{"favicon.ico"}
	HandleResource(Context)
}

func HandleIndex(Context *THttpRequestContext) {
	Redirect(Context, "/account")
}

func HandleAccount(Context *THttpRequestContext) {
	if Context.AccountID > 0 {
		RenderAccountSummary(Context)
		return
	}

	switch Context.Request.Method {
	case http.MethodGet:
		RenderAccountLogin(Context)
	case http.MethodPost:
		Account := Context.Request.FormValue("account")
		Password := Context.Request.FormValue("password")

		// TODO(fusion): Other input checks?
		if Account == "" || Password == "" {
			RenderMessage(Context, "Login Error", "Account or password is not correct.")
			return
		}

		AccountID, Err := strconv.Atoi(Account)
		if Err != nil {
			g_LogErr.Printf("Failed to parse account id: %d", Err)
			RenderMessage(Context, "Login Error", "Account or password is not correct.")
			return
		}

		Result := CheckAccountPassword(AccountID, Password, Context.IPAddress)
		switch Result {
		case 0:
			// NOTE(fusion): Invalidate account's cached data just in case.
			InvalidateAccountCachedData(AccountID)
			SessionStart(Context, AccountID)
			RenderAccountSummary(Context)
		case 1, 2:
			RenderMessage(Context, "Login Error", "Account or password is not correct.")
		case 3:
			RenderMessage(Context, "Login Error", "Account disabled for five minutes.")
		case 4:
			RenderMessage(Context, "Login Error", "IP address blocked for 30 minutes.")
		case 5:
			RenderMessage(Context, "Login Error", "Your account is banished.")
		case 6:
			RenderMessage(Context, "Login Error", "Your IP address is banished.")
		default:
			RenderMessage(Context, "Login Error", "Internal error.")
		}
	default:
		NotFound(Context)
	}
}

func HandleAccountLogout(Context *THttpRequestContext) {
	SessionEnd(Context)
	Redirect(Context, "/account")
}

func HandleAccountCreate(Context *THttpRequestContext) {
	if Context.AccountID > 0 {
		Redirect(Context, "/account")
		return
	}

	switch Context.Request.Method {
	case http.MethodGet:
		RenderAccountCreate(Context)
	case http.MethodPost:
		Account := Context.Request.FormValue("account")
		Email := Context.Request.FormValue("email")
		Password := Context.Request.FormValue("password")

		if Account == "" || Email == "" || Password == "" {
			RenderMessage(Context, "Create Account Error", "All inputs are REQUIRED.")
			return
		}

		AccountID, Err := strconv.Atoi(Account)
		if Err != nil {
			g_LogErr.Printf("Failed to parse account id: %d", Err)
			RenderMessage(Context, "Create Account Error", "Invalid account number.")
			return
		}

		if Email != Context.Request.FormValue("email_confirm") {
			RenderMessage(Context, "Create Account Error", "Emails don't match.")
			return
		}

		if Password != Context.Request.FormValue("password_confirm") {
			RenderMessage(Context, "Create Account Error", "Passwords don't match.")
			return
		}

		if AccountID < 100000 || AccountID > 999999999 {
			RenderMessage(Context, "Create Account Error", "Account number must contain 6-9 digits.")
			return
		}

		// TODO(fusion): Proper email and password checking.
		if len(Password) < 8 {
			RenderMessage(Context, "Create Account Error", "Password must contain at least 8 characters.")
			return
		}

		Result := CreateAccount(AccountID, Email, Password)
		switch Result {
		case 0:
			RenderMessage(Context, "Account Created",
				"Your account has been created. Head back to the login page to access it.")
		case 1:
			RenderMessage(Context, "Create Account Error", "An account with that number already exists.")
		case 2:
			RenderMessage(Context, "Create Account Error", "An account with that email already exists.")
		default:
			RenderMessage(Context, "Create Account Error", "Internal error.")
		}
	default:
		NotFound(Context)
	}
}

func HandleAccountRecover(Context *THttpRequestContext) {
	if Context.AccountID > 0 {
		Redirect(Context, "/account")
		return
	}

	switch Context.Request.Method {
	case http.MethodGet:
		RenderAccountRecover(Context)
	default:
		NotFound(Context)
	}
}

func HandleCharacterCreate(Context *THttpRequestContext) {
	if Context.AccountID <= 0 {
		Redirect(Context, "/account")
		return
	}

	switch Context.Request.Method {
	case http.MethodGet:
		RenderCharacterCreate(Context)
	case http.MethodPost:
		World := strings.TrimSpace(Context.Request.FormValue("world"))
		if World == "" || GetWorld(World) == nil {
			RenderMessage(Context, "Create Character Error", "Invalid world.")
			return
		}

		// TODO(fusion): Proper name checking.
		Name := strings.TrimSpace(Context.Request.FormValue("name"))
		if len(Name) < 4 || len(Name) > 25 {
			RenderMessage(Context, "Create Character Error", "Name must contain between 8 and 25 characters.")
			return
		}

		Sex, Err := strconv.Atoi(Context.Request.FormValue("sex"))
		if Err != nil || (Sex != 1 && Sex != 2) {
			if Err != nil {
				g_LogErr.Printf("Failed to parse character sex: %v", Err)
			}
			RenderMessage(Context, "Create Character Error", "Invalid sex.")
			return
		}

		Result := CreateCharacter(World, Context.AccountID, Name, Sex)
		switch Result {
		case 0:
			// NOTE(fusion): Invalidate account's cached data so the new character
			// is displayed in the account's summary.
			InvalidateAccountCachedData(Context.AccountID)
			RenderMessage(Context, "Character Created",
				fmt.Sprintf("And so, %v was brought into this world. May fortune"+
					" favor your blade and guide your steps through the trials ahead.",
					Name))
		case 1:
			RenderMessage(Context, "Create Character Error",
				"Weirdly enough, the selected world doesn't exist. What have you been up to?")
		case 2:
			RenderMessage(Context, "Create Character Error",
				"Weirdly enough, your account doesn't exist. What have you been up to?")
		case 3:
			RenderMessage(Context, "Create Character Error", "A character with that name already exists.")
		default:
			RenderMessage(Context, "Create Character Error", "Internal error.")
		}
	default:
		NotFound(Context)
	}
}

func HandleCharacterProfile(Context *THttpRequestContext) {
	QueryValues := Context.Request.URL.Query()
	CharacterName := QueryValues.Get("name")
	if CharacterName == "" {
		RenderCharacterProfile(Context, nil)
	} else {
		Result, Character := GetCharacterProfile(CharacterName)
		switch Result {
		case 0:
			RenderCharacterProfile(Context, &Character)
		case 1:
			RenderMessage(Context, "Search Error", "A character with that name doesn't exist.")
		default:
			RenderMessage(Context, "Search Error", "Internal error.")
		}
	}
}

func HandleKillStatistics(Context *THttpRequestContext) {
	QueryValues := Context.Request.URL.Query()
	WorldName := QueryValues.Get("world")
	if WorldName == "" || GetWorld(WorldName) == nil {
		Redirect(Context, "/world")
	} else {
		RenderKillStatistics(Context, WorldName)
	}
}

func HandleWorld(Context *THttpRequestContext) {
	QueryValues := Context.Request.URL.Query()
	WorldName := QueryValues.Get("name")
	if WorldName == "" || GetWorld(WorldName) == nil {
		RenderWorldList(Context)
	} else {
		RenderWorldInfo(Context, WorldName)
	}
}

func main() {
	g_Log.Print("Tibia Web Server v0.1")
	if !ReadConfig("config.cfg", WebKVCallback) {
		return
	}

	defer ExitQuery()
	defer ExitMail()
	defer ExitTemplates()
	if !InitQuery() || !InitMail() || !InitTemplates() {
		return
	}

	Router := THttpRouter{}
	Router.Add("GET", "/res/", HandleResource)
	Router.Add("GET", "/favicon.ico", HandleFavicon)
	Router.Add("GET", "/", HandleIndex)
	Router.Add("GET", "/index", HandleIndex)
	Router.Add("GET", "/account", HandleAccount)
	Router.Add("POST", "/account", HandleAccount)
	Router.Add("GET", "/account/logout", HandleAccountLogout)
	Router.Add("GET", "/account/create", HandleAccountCreate)
	Router.Add("POST", "/account/create", HandleAccountCreate)
	Router.Add("GET", "/account/recover", HandleAccountRecover)
	Router.Add("POST", "/account/recover", HandleAccountRecover)
	Router.Add("GET", "/character/create", HandleCharacterCreate)
	Router.Add("POST", "/character/create", HandleCharacterCreate)
	Router.Add("GET", "/character", HandleCharacterProfile)
	Router.Add("GET", "/killstatistics", HandleKillStatistics)
	Router.Add("GET", "/world", HandleWorld)
	Router.NotFound = NotFound

	// NOTE(fusion): Force the server to run on IPv4 because that is the only
	// format the query manager currently handles. Trying to use IPv6 will cause
	// queries to fail.
	if FileExists(g_HttpsCertFile) && FileExists(g_HttpsKeyFile) {
		Listener, Err := net.Listen("tcp4", JoinHostPort("", g_HttpsPort))
		if Err != nil {
			g_LogErr.Printf("Failed to listen to HTTPS port %v: %v", g_HttpsPort, Err)
			return
		}

		g_Log.Printf("Running over HTTPS on port %v", g_HttpsPort)
		g_Log.Print(http.ServeTLS(Listener, &Router, g_HttpsCertFile, g_HttpsKeyFile))
	} else {
		g_LogWarn.Print("The server is setup to run over HTTP which is NOT SECURE" +
			" and prone to a man-in-the-middle or eavesdropping attack. This setup" +
			" may only be used for TESTING.")

		Listener, Err := net.Listen("tcp4", JoinHostPort("", g_HttpPort))
		if Err != nil {
			g_LogErr.Printf("Failed to listen to HTTP port %v: %v", g_HttpPort, Err)
			return
		}

		g_Log.Printf("Running over HTTP on port %v", g_HttpPort)
		g_Log.Print(http.Serve(Listener, &Router))
	}
}
