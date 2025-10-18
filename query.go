package main

import (
	"encoding/binary"
	"net"
	"strings"
	"sync"
	"time"
)

// TQueryManagerConnection
// ==============================================================================
const (
	APPLICATION_TYPE_GAME  = 1
	APPLICATION_TYPE_LOGIN = 2
	APPLICATION_TYPE_WEB   = 3
)

const (
	QUERY_STATUS_OK     = 0
	QUERY_STATUS_ERROR  = 1
	QUERY_STATUS_FAILED = 3
)

const (
	// TODO(fusion): There are newly created queries to support basic account
	// management. A production ready website would need even more queries to
	// allow account activation, recovery, deletion, password change, character
	// deletion, etc...

	QUERY_LOGIN                  = 0
	QUERY_CHECK_ACCOUNT_PASSWORD = 10
	QUERY_CREATE_ACCOUNT         = 100
	QUERY_CREATE_CHARACTER       = 101
	QUERY_GET_ACCOUNT_SUMMARY    = 102
	QUERY_GET_CHARACTER_PROFILE  = 103
	QUERY_GET_WORLDS             = 150
	QUERY_GET_ONLINE_CHARACTERS  = 151
	QUERY_GET_KILL_STATISTICS    = 152
)

type (
	TWorld struct {
		Name             string
		Type             string
		NumPlayers       int
		MaxPlayers       int
		OnlinePeak       int
		OnlinePeakTimestamp int
		LastStartup      int
		LastShutdown     int
	}

	TAccountSummary struct {
		AccountID          int
		Email              string
		PremiumDays        int
		PendingPremiumDays int
		Deleted            bool
		Characters         []TCharacterSummary
	}

	TCharacterSummary struct {
		Name       string
		World      string
		Level      int
		Profession string
		Online     bool
		Deleted    bool
	}

	TCharacterProfile struct {
		Name        string
		World       string
		Sex         int
		Guild       string
		Rank        string
		Title       string
		Level       int
		Profession  string
		Residence   string
		LastLogin   int
		PremiumDays int
		Online      bool
		Deleted     bool
	}

	TKillStatistics struct {
		RaceName      string
		TimesKilled   int
		PlayersKilled int
	}

	TOnlineCharacter struct {
		Name       string
		Level      int
		Profession string
	}

	TAccountCacheEntry struct {
		AccountID  int
		Result     int
		Data       TAccountSummary
		LastAccess time.Time
	}

	TCharacterCacheEntry struct {
		CharacterName string
		Result        int
		Data          TCharacterProfile
		LastAccess    time.Time
	}

	TKillStatisticsCacheEntry struct {
		World       string
		Data        []TKillStatistics
		RefreshTime time.Time
	}

	TOnlineCharactersCacheEntry struct {
		World       string
		Data        []TOnlineCharacter
		RefreshTime time.Time
	}

	TQueryManagerConnection struct {
		Handle net.Conn
	}
)

func (Connection *TQueryManagerConnection) Connect() bool {
	if Connection.Handle != nil {
		g_LogErr.Print("Already connected")
		return false
	}

	var Err error
	QueryManagerAddress := JoinHostPort(g_QueryManagerHost, g_QueryManagerPort)
	Connection.Handle, Err = net.Dial("tcp4", QueryManagerAddress)
	if Err != nil {
		g_LogErr.Print(Err)
		return false
	}

	var LoginBuffer [1024]byte
	WriteBuffer := Connection.PrepareQuery(QUERY_LOGIN, LoginBuffer[:])
	WriteBuffer.Write8(APPLICATION_TYPE_WEB)
	WriteBuffer.WriteString(g_QueryManagerPassword)
	Status, _ := Connection.ExecuteQuery(false, &WriteBuffer)
	if Status != QUERY_STATUS_OK {
		Connection.Disconnect()
		g_LogErr.Printf("Failed to login to query manager (%v)", Status)
		return false
	}

	return true
}

func (Connection *TQueryManagerConnection) Disconnect() {
	if Connection.Handle != nil {
		if Err := Connection.Handle.Close(); Err != nil {
			g_LogErr.Print(Err)
		}
		Connection.Handle = nil
	}
}

func (Connection *TQueryManagerConnection) PrepareQuery(QueryType int, Buffer []byte) TWriteBuffer {
	WriteBuffer := TWriteBuffer{Buffer: Buffer, Position: 0}
	WriteBuffer.Write16(0) // Request Size
	WriteBuffer.Write8(uint8(QueryType))
	return WriteBuffer
}

func (Connection *TQueryManagerConnection) ExecuteQuery(AutoReconnect bool, WriteBuffer *TWriteBuffer) (Status int, ReadBuffer TReadBuffer) {
	// IMPORTANT(fusion): Different from the C++ version, there is no connection
	// buffer, and the response is read into the same buffer used by `WriteBuffer`,
	// to avoid moving data around when reconnecting in the middle of a query.
	// TODO(fusion): Maybe join `TWriteBuffer` and `TReadBuffer` into `TQueryBuffer`
	// to avoid confusion on how this function operates?
	if WriteBuffer == nil || WriteBuffer.Position <= 2 {
		panic("write buffer is empty")
	}

	RequestSize := WriteBuffer.Position - 2
	if RequestSize < 0xFFFF {
		WriteBuffer.Rewrite16(0, uint16(RequestSize))
	} else {
		WriteBuffer.Rewrite16(0, 0xFFFF)
		WriteBuffer.Insert32(2, uint32(RequestSize))
	}

	Status = QUERY_STATUS_FAILED
	if WriteBuffer.Overflowed() {
		g_LogErr.Print("Write buffer overflowed")
		return
	}

	const MaxAttempts = 2
	Buffer := WriteBuffer.Buffer
	WriteSize := WriteBuffer.Position
	for Attempt := 1; true; Attempt += 1 {
		if Connection.Handle == nil && (!AutoReconnect || !Connection.Connect()) {
			return
		}

		if _, Err := Connection.Handle.Write(Buffer[:WriteSize]); Err != nil {
			Connection.Disconnect()
			if Attempt >= MaxAttempts {
				g_LogErr.Printf("Failed to write request: %v", Err)
				return
			}
			continue
		}

		var Help [4]byte
		if _, Err := Connection.Handle.Read(Help[:2]); Err != nil {
			Connection.Disconnect()
			if Attempt >= MaxAttempts {
				g_LogErr.Printf("Failed to read response size: %v", Err)
				return
			}
			continue
		}

		ResponseSize := int(binary.LittleEndian.Uint16(Help[:2]))
		if ResponseSize == 0xFFFF {
			if _, Err := Connection.Handle.Read(Help[:]); Err != nil {
				Connection.Disconnect()
				g_LogErr.Printf("Failed to read response extended size: %v", Err)
				return
			}

			ResponseSize = int(binary.LittleEndian.Uint32(Help[:]))
		}

		if ResponseSize <= 0 || ResponseSize > len(Buffer) {
			Connection.Disconnect()
			g_LogErr.Printf("Invalid response size %v (BufferSize: %v)",
				ResponseSize, len(Buffer))
			return
		}

		if _, Err := Connection.Handle.Read(Buffer[:ResponseSize]); Err != nil {
			Connection.Disconnect()
			g_LogErr.Printf("Failed to read response: %v", Err)
			return
		}

		ReadBuffer = TReadBuffer{
			Buffer:   Buffer,
			Position: 0,
		}
		Status = int(ReadBuffer.Read8())
		return
	}

	// NOTE(fusion): The compiler complains there is no return statement here
	// but the loop above can only exit by returning from the function which
	// make anything after it UNREACHABLE.
	return
}

func (Connection *TQueryManagerConnection) CheckAccountPassword(AccountID int, Password, IPAddress string) (Result int) {
	var Buffer [1024]byte
	WriteBuffer := Connection.PrepareQuery(QUERY_CHECK_ACCOUNT_PASSWORD, Buffer[:])
	WriteBuffer.Write32(uint32(AccountID))
	WriteBuffer.WriteString(Password)
	WriteBuffer.WriteString(IPAddress)
	Status, ReadBuffer := Connection.ExecuteQuery(true, &WriteBuffer)
	Result = -1
	switch Status {
	case QUERY_STATUS_OK:
		Result = 0
	case QUERY_STATUS_ERROR:
		ErrorCode := int(ReadBuffer.Read8())
		if ErrorCode >= 1 && ErrorCode <= 4 {
			Result = ErrorCode
		} else {
			g_LogErr.Printf("Invalid error code %v", ErrorCode)
		}
	default:
		g_LogErr.Printf("Request failed (%v)", Status)
	}
	return
}

func (Connection *TQueryManagerConnection) CreateAccount(AccountID int, Email string, Password string) (Result int) {
	var Buffer [1024]byte
	WriteBuffer := Connection.PrepareQuery(QUERY_CREATE_ACCOUNT, Buffer[:])
	WriteBuffer.Write32(uint32(AccountID))
	WriteBuffer.WriteString(Email)
	WriteBuffer.WriteString(Password)
	Status, ReadBuffer := Connection.ExecuteQuery(true, &WriteBuffer)
	Result = -1
	switch Status {
	case QUERY_STATUS_OK:
		Result = 0
	case QUERY_STATUS_ERROR:
		ErrorCode := int(ReadBuffer.Read8())
		if ErrorCode >= 1 && ErrorCode <= 2 {
			Result = ErrorCode
		} else {
			g_LogErr.Printf("Invalid error code %v", ErrorCode)
		}
	default:
		g_LogErr.Printf("Request failed (%v)", Status)
	}
	return
}

func (Connection *TQueryManagerConnection) CreateCharacter(World string, AccountID int, Name string, Sex int) (Result int) {
	var Buffer [1024]byte
	WriteBuffer := Connection.PrepareQuery(QUERY_CREATE_CHARACTER, Buffer[:])
	WriteBuffer.WriteString(World)
	WriteBuffer.Write32(uint32(AccountID))
	WriteBuffer.WriteString(Name)
	WriteBuffer.Write8(uint8(Sex))
	Status, ReadBuffer := Connection.ExecuteQuery(true, &WriteBuffer)
	Result = -1
	switch Status {
	case QUERY_STATUS_OK:
		Result = 0
	case QUERY_STATUS_ERROR:
		ErrorCode := int(ReadBuffer.Read8())
		if ErrorCode >= 1 && ErrorCode <= 3 {
			Result = ErrorCode
		} else {
			g_LogErr.Printf("Invalid error code %v", ErrorCode)
		}
	default:
		g_LogErr.Printf("Request failed (%v)", Status)
	}
	return
}

func (Connection *TQueryManagerConnection) GetAccountSummary(AccountID int) (Result int, Account TAccountSummary) {
	var Buffer [16384]byte
	WriteBuffer := Connection.PrepareQuery(QUERY_GET_ACCOUNT_SUMMARY, Buffer[:])
	WriteBuffer.Write32(uint32(AccountID))
	Status, ReadBuffer := Connection.ExecuteQuery(true, &WriteBuffer)
	Result = -1
	switch Status {
	case QUERY_STATUS_OK:
		Result = 0
		Account.AccountID = AccountID
		Account.Email = ReadBuffer.ReadString()
		Account.PremiumDays = int(ReadBuffer.Read16())
		Account.PendingPremiumDays = int(ReadBuffer.Read16())
		Account.Deleted = ReadBuffer.ReadFlag()
		NumCharacters := int(ReadBuffer.Read8())
		if NumCharacters > 0 {
			Account.Characters = make([]TCharacterSummary, NumCharacters)
			for Index := range Account.Characters {
				Account.Characters[Index].Name = ReadBuffer.ReadString()
				Account.Characters[Index].World = ReadBuffer.ReadString()
				Account.Characters[Index].Level = int(ReadBuffer.Read16())
				Account.Characters[Index].Profession = ReadBuffer.ReadString()
				Account.Characters[Index].Online = ReadBuffer.ReadFlag()
				Account.Characters[Index].Deleted = ReadBuffer.ReadFlag()
			}
		}
	case QUERY_STATUS_ERROR:
		ErrorCode := int(ReadBuffer.Read8())
		if ErrorCode >= 1 && ErrorCode <= 4 {
			Result = ErrorCode
		} else {
			g_LogErr.Printf("Invalid error code %v", ErrorCode)
		}
	default:
		g_LogErr.Printf("Request failed (%v)", Status)
	}
	return
}

func (Connection *TQueryManagerConnection) GetCharacterProfile(CharacterName string) (Result int, Character TCharacterProfile) {
	var Buffer [16384]byte
	WriteBuffer := Connection.PrepareQuery(QUERY_GET_CHARACTER_PROFILE, Buffer[:])
	WriteBuffer.WriteString(CharacterName)
	Status, ReadBuffer := Connection.ExecuteQuery(true, &WriteBuffer)
	Result = -1
	switch Status {
	case QUERY_STATUS_OK:
		Result = 0
		Character.Name = ReadBuffer.ReadString()
		Character.World = ReadBuffer.ReadString()
		Character.Sex = int(ReadBuffer.Read8())
		Character.Guild = ReadBuffer.ReadString()
		Character.Rank = ReadBuffer.ReadString()
		Character.Title = ReadBuffer.ReadString()
		Character.Level = int(ReadBuffer.Read16())
		Character.Profession = ReadBuffer.ReadString()
		Character.Residence = ReadBuffer.ReadString()
		Character.LastLogin = int(ReadBuffer.Read32())
		Character.PremiumDays = int(ReadBuffer.Read16())
		Character.Online = ReadBuffer.ReadFlag()
		Character.Deleted = ReadBuffer.ReadFlag()
	case QUERY_STATUS_ERROR:
		ErrorCode := int(ReadBuffer.Read8())
		if ErrorCode == 1 {
			Result = ErrorCode
		} else {
			g_LogErr.Printf("Invalid error code %v", ErrorCode)
		}
	default:
		g_LogErr.Printf("Request failed (%v)", Status)
	}
	return
}

func (Connection *TQueryManagerConnection) GetWorlds() (Result int, Worlds []TWorld) {
	var Buffer [16384]byte
	WriteBuffer := Connection.PrepareQuery(QUERY_GET_WORLDS, Buffer[:])
	Status, ReadBuffer := Connection.ExecuteQuery(true, &WriteBuffer)
	Result = -1
	switch Status {
	case QUERY_STATUS_OK:
		Result = 0
		NumWorlds := int(ReadBuffer.Read8())
		if NumWorlds > 0 {
			Worlds = make([]TWorld, NumWorlds)
			for Index := range Worlds {
				Worlds[Index].Name = ReadBuffer.ReadString()
				Worlds[Index].Type = WorldTypeString(int(ReadBuffer.Read8()))
				Worlds[Index].NumPlayers = int(ReadBuffer.Read16())
				Worlds[Index].MaxPlayers = int(ReadBuffer.Read16())
				Worlds[Index].OnlinePeak = int(ReadBuffer.Read16())
				Worlds[Index].OnlinePeakTimestamp = int(ReadBuffer.Read32())
				Worlds[Index].LastStartup = int(ReadBuffer.Read32())
				Worlds[Index].LastShutdown = int(ReadBuffer.Read32())
			}
		}
	default:
		g_LogErr.Printf("Request failed (%v)", Status)
	}
	return
}

func (Connection *TQueryManagerConnection) GetOnlineCharacters(World string) (Result int, Characters []TOnlineCharacter) {
	var Buffer [65536]byte
	WriteBuffer := Connection.PrepareQuery(QUERY_GET_ONLINE_CHARACTERS, Buffer[:])
	WriteBuffer.WriteString(World)
	Status, ReadBuffer := Connection.ExecuteQuery(true, &WriteBuffer)
	Result = -1
	switch Status {
	case QUERY_STATUS_OK:
		Result = 0
		NumCharacters := int(ReadBuffer.Read16())
		if NumCharacters > 0 {
			Characters = make([]TOnlineCharacter, NumCharacters)
			for Index := 0; Index < NumCharacters; Index += 1 {
				Characters[Index].Name = ReadBuffer.ReadString()
				Characters[Index].Level = int(ReadBuffer.Read16())
				Characters[Index].Profession = ReadBuffer.ReadString()
			}
		}
	default:
		g_LogErr.Printf("Request failed (%v)", Status)
	}
	return
}

func (Connection *TQueryManagerConnection) GetKillStatistics(World string) (Result int, Stats []TKillStatistics) {
	var Buffer [65536]byte
	WriteBuffer := Connection.PrepareQuery(QUERY_GET_KILL_STATISTICS, Buffer[:])
	WriteBuffer.WriteString(World)
	Status, ReadBuffer := Connection.ExecuteQuery(true, &WriteBuffer)
	Result = -1
	switch Status {
	case QUERY_STATUS_OK:
		Result = 0
		NumStats := int(ReadBuffer.Read16())
		if NumStats > 0 {
			Stats = make([]TKillStatistics, NumStats)
			for Index := 0; Index < NumStats; Index += 1 {
				Stats[Index].RaceName = ReadBuffer.ReadString()
				Stats[Index].PlayersKilled = int(ReadBuffer.Read32())
				Stats[Index].TimesKilled = int(ReadBuffer.Read32())
			}
		}
	default:
		g_LogErr.Printf("Request failed (%v)", Status)
	}
	return
}

// Query Subsystem
// ==============================================================================
var (
	g_QueryManagerMutex      sync.Mutex
	g_QueryManagerConnection TQueryManagerConnection

	g_AccountCache          []TAccountCacheEntry
	g_CharacterCache        []TCharacterCacheEntry
	g_WorldCache            []TWorld
	g_WorldCacheRefreshTime time.Time
	g_OnlineCharactersCache []TOnlineCharactersCacheEntry
	g_KillStatisticsCache   []TKillStatisticsCacheEntry
)

func InitQuery() bool {
	g_Log.Printf("QueryManagerHost: %v", g_QueryManagerHost)
	g_Log.Printf("QueryManagerPort: %v", g_QueryManagerPort)
	g_Log.Printf("MaxCachedAccounts: %v", g_MaxCachedAccounts)
	g_Log.Printf("MaxCachedCharacters: %v", g_MaxCachedCharacters)
	g_Log.Printf("CharacterRefreshInterval: %v", g_CharacterRefreshInterval)
	g_Log.Printf("WorldRefreshInterval: %v", g_WorldRefreshInterval)

	Result := g_QueryManagerConnection.Connect()
	if !Result {
		g_LogErr.Print("Failed to connect to query manager")
	}
	return Result
}

func ExitQuery() {
	g_QueryManagerConnection.Disconnect()
}

func CheckAccountPassword(AccountID int, Password, IPAddress string) int {
	g_QueryManagerMutex.Lock()
	defer g_QueryManagerMutex.Unlock()
	return g_QueryManagerConnection.CheckAccountPassword(AccountID, Password, IPAddress)
}

func CreateAccount(AccountID int, Email string, Password string) int {
	g_QueryManagerMutex.Lock()
	defer g_QueryManagerMutex.Unlock()
	return g_QueryManagerConnection.CreateAccount(AccountID, Email, Password)
}

func CreateCharacter(World string, AccountID int, Name string, Sex int) int {
	g_QueryManagerMutex.Lock()
	defer g_QueryManagerMutex.Unlock()
	return g_QueryManagerConnection.CreateCharacter(World, AccountID, Name, Sex)
}

func GetAccountSummary(AccountID int) (Result int, Account TAccountSummary) {
	g_QueryManagerMutex.Lock()
	defer g_QueryManagerMutex.Unlock()

	if g_AccountCache == nil {
		g_AccountCache = make([]TAccountCacheEntry, g_MaxCachedAccounts)
	}

	var Entry *TAccountCacheEntry
	LeastRecentlyUsedIndex := 0
	LeastRecentlyUsedTime := g_AccountCache[0].LastAccess
	for Index := 0; Index < len(g_AccountCache); Index += 1 {
		Current := &g_AccountCache[Index]

		// NOTE(fusion): Account data itself shouldn't change over time unless
		// we do it ourselves, in which case `InvalidateAccountCachedData` is
		// used to invalidate the cache entry. The problem is that the account
		// summary also includes character data which will change, depending on
		// activities on the game server.
		if time.Since(Current.LastAccess) >= g_CharacterRefreshInterval {
			*Current = TAccountCacheEntry{}
		}

		if Current.LastAccess.Before(LeastRecentlyUsedTime) {
			LeastRecentlyUsedIndex = Index
			LeastRecentlyUsedTime = Current.LastAccess
		}

		if Current.AccountID == AccountID {
			Entry = Current
			break
		}
	}

	if Entry == nil {
		Result, Account = g_QueryManagerConnection.GetAccountSummary(AccountID)
		if Result == 0 {
			Entry = &g_AccountCache[LeastRecentlyUsedIndex]
			Entry.AccountID = AccountID
			Entry.Data = Account
			Entry.LastAccess = time.Now()
		}
	} else {
		Result = 0
		Account = Entry.Data
		Entry.LastAccess = time.Now()
	}

	return
}

func InvalidateAccountCachedData(AccountID int) {
	g_QueryManagerMutex.Lock()
	defer g_QueryManagerMutex.Unlock()
	for Index := 0; Index < len(g_AccountCache); Index += 1 {
		if g_AccountCache[Index].AccountID == AccountID {
			g_AccountCache[Index] = TAccountCacheEntry{}
			break
		}
	}
}

func GetCharacterProfile(CharacterName string) (Result int, Character TCharacterProfile) {
	g_QueryManagerMutex.Lock()
	defer g_QueryManagerMutex.Unlock()

	if g_CharacterCache == nil {
		g_CharacterCache = make([]TCharacterCacheEntry, g_MaxCachedCharacters)
	}

	var Entry *TCharacterCacheEntry
	LeastRecentlyUsedIndex := 0
	LeastRecentlyUsedTime := g_CharacterCache[0].LastAccess
	for Index := 0; Index < len(g_CharacterCache); Index += 1 {
		Current := &g_CharacterCache[Index]

		if time.Since(Current.LastAccess) >= g_CharacterRefreshInterval {
			*Current = TCharacterCacheEntry{}
		}

		if Current.LastAccess.Before(LeastRecentlyUsedTime) {
			LeastRecentlyUsedIndex = Index
			LeastRecentlyUsedTime = Current.LastAccess
		}

		if strings.EqualFold(Current.CharacterName, CharacterName) {
			Entry = Current
			break
		}
	}

	if Entry == nil {
		Result, Character = g_QueryManagerConnection.GetCharacterProfile(CharacterName)
		Entry = &g_CharacterCache[LeastRecentlyUsedIndex]
		Entry.CharacterName = CharacterName
		Entry.Result = Result
		Entry.Data = Character
		Entry.LastAccess = time.Now()
	} else {
		Result = Entry.Result
		Character = Entry.Data
		Entry.LastAccess = time.Now()
	}

	return
}

func GetWorlds() []TWorld {
	g_QueryManagerMutex.Lock()
	defer g_QueryManagerMutex.Unlock()
	if time.Until(g_WorldCacheRefreshTime) <= 0 {
		// IMPORTANT(fusion): `GetWorlds` will return a FRESH slice. This will
		// prevent race conditions regarding any previous world slice, assuming
		// we're only reading from them.
		Result, Worlds := g_QueryManagerConnection.GetWorlds()
		if Result == 0 {
			g_WorldCache = Worlds
			g_WorldCacheRefreshTime = time.Now().Add(g_WorldRefreshInterval)
		}
	}
	return g_WorldCache
}

func GetWorld(World string) *TWorld {
	Worlds := GetWorlds()
	for Index := range Worlds {
		if strings.EqualFold(Worlds[Index].Name, World) {
			return &Worlds[Index]
		}
	}
	return nil
}

func GetOnlineCharacters(World string) []TOnlineCharacter {
	g_QueryManagerMutex.Lock()
	defer g_QueryManagerMutex.Unlock()

	var Entry *TOnlineCharactersCacheEntry
	for Index := 0; Index < len(g_OnlineCharactersCache); Index += 1 {
		Current := &g_OnlineCharactersCache[Index]
		if time.Until(Current.RefreshTime) <= 0 {
			g_OnlineCharactersCache = SwapAndPop(g_OnlineCharactersCache, Index)
			Index -= 1
			continue
		}

		if strings.EqualFold(Current.World, World) {
			Entry = Current
			break
		}
	}

	if Entry == nil {
		Result, Characters := g_QueryManagerConnection.GetOnlineCharacters(World)
		if Result == 0 {
			g_OnlineCharactersCache = append(g_OnlineCharactersCache, TOnlineCharactersCacheEntry{})
			Entry = &g_OnlineCharactersCache[len(g_OnlineCharactersCache)-1]
			Entry.World = World
			Entry.Data = Characters
			Entry.RefreshTime = time.Now().Add(g_WorldRefreshInterval)
		}
	}

	if Entry != nil {
		return Entry.Data
	} else {
		return nil
	}
}

func GetKillStatistics(World string) []TKillStatistics {
	g_QueryManagerMutex.Lock()
	defer g_QueryManagerMutex.Unlock()

	var Entry *TKillStatisticsCacheEntry
	for Index := 0; Index < len(g_KillStatisticsCache); Index += 1 {
		Current := &g_KillStatisticsCache[Index]
		if time.Until(Current.RefreshTime) <= 0 {
			g_KillStatisticsCache = SwapAndPop(g_KillStatisticsCache, Index)
			Index -= 1
			continue
		}

		if strings.EqualFold(Current.World, World) {
			Entry = Current
			break
		}
	}

	if Entry == nil {
		Result, Stats := g_QueryManagerConnection.GetKillStatistics(World)
		if Result == 0 {
			g_KillStatisticsCache = append(g_KillStatisticsCache, TKillStatisticsCacheEntry{})
			Entry = &g_KillStatisticsCache[len(g_KillStatisticsCache)-1]
			Entry.World = World
			Entry.Data = Stats
			Entry.RefreshTime = time.Now().Add(g_WorldRefreshInterval)
		}
	}

	if Entry != nil {
		return Entry.Data
	} else {
		return nil
	}
}
