package main

import (
	"fmt"
	"html/template"
	"io"
	"net/http"
	"strconv"
)

type (
	CommonTmplData struct {
		Title     string
		AccountID int
	}

	GenericTmplData struct {
		Common CommonTmplData
	}

	AccountTmplData struct {
		Common  CommonTmplData
		Account *TAccountSummary
	}

	CharacterTmplData struct {
		Common    CommonTmplData
		Character *TCharacterProfile
	}

	KillStatisticsTmplData struct {
		Common         CommonTmplData
		World          *TWorld
		KillStatistics []TKillStatistics
	}

	WorldTmplData struct {
		Common           CommonTmplData
		World            *TWorld
		OnlineCharacters []TOnlineCharacter
	}

	WorldListTmplData struct {
		Common CommonTmplData
		Worlds []TWorld
	}

	MessageTmplData struct {
		Common  CommonTmplData
		Heading string
		Message string
	}
)

var (
	g_Templates *template.Template
)

func InitTemplates() bool {
	var Err error

	CustomFuncs := template.FuncMap{
		"FormatTimestamp": FormatTimestamp,
		"FormatDurationSince": FormatDurationSince,
	}

	g_Templates, Err = template.New("").Funcs(CustomFuncs).ParseGlob("templates/*.tmpl")
	if Err != nil {
		g_LogErr.Printf("Failed to parse templates: %v", Err)
		return false
	}
	return true
}

func ExitTemplates() {
	g_Templates = nil
}

func ExecuteTemplate(Writer io.Writer, FileName string, Data any) {
	Err := g_Templates.ExecuteTemplate(Writer, FileName, Data)
	if Err != nil {
		g_LogErr.Printf("Failed to execute template \"%v\": %v", FileName, Err)
	}
}

func RenderRequestError(Context *THttpRequestContext, Status int) {
	StatusText := http.StatusText(Status)
	ExecuteTemplate(Context.Writer, "message.tmpl",
		MessageTmplData{
			Common: CommonTmplData{
				Title:     StatusText,
				AccountID: Context.AccountID,
			},
			Heading: strconv.Itoa(Status),
			Message: StatusText,
		})
}

func RenderMessage(Context *THttpRequestContext, Heading string, Message string) {
	ExecuteTemplate(Context.Writer, "message.tmpl",
		MessageTmplData{
			Common: CommonTmplData{
				Title:     Heading,
				AccountID: Context.AccountID,
			},
			Heading: Heading,
			Message: Message,
		})
}

func RenderAccountSummary(Context *THttpRequestContext) {
	Data := AccountTmplData{
		Common: CommonTmplData{
			Title:     "Account Summary",
			AccountID: Context.AccountID,
		},
		Account: nil,
	}

	Result, Account := GetAccountSummary(Context.AccountID)
	if Result == 0 {
		Data.Account = &Account
	}

	ExecuteTemplate(Context.Writer, "account_summary.tmpl", Data)
}

func RenderAccountLogin(Context *THttpRequestContext) {
	ExecuteTemplate(Context.Writer, "account_login.tmpl",
		GenericTmplData{
			Common: CommonTmplData{
				Title:     "Login",
				AccountID: Context.AccountID,
			},
		})
}

func RenderAccountCreate(Context *THttpRequestContext) {
	ExecuteTemplate(Context.Writer, "account_create.tmpl",
		GenericTmplData{
			Common: CommonTmplData{
				Title:     "Create Account",
				AccountID: Context.AccountID,
			},
		})
}

func RenderAccountRecover(Context *THttpRequestContext) {
	ExecuteTemplate(Context.Writer, "account_recover.tmpl",
		GenericTmplData{
			Common: CommonTmplData{
				Title:     "Recover Account",
				AccountID: Context.AccountID,
			},
		})
}

func RenderCharacterCreate(Context *THttpRequestContext) {
	ExecuteTemplate(Context.Writer, "character_create.tmpl",
		WorldListTmplData{
			Common: CommonTmplData{
				Title:     "Create Character",
				AccountID: Context.AccountID,
			},
			Worlds: GetWorlds(),
		})
}

func RenderCharacterProfile(Context *THttpRequestContext, Character *TCharacterProfile) {
	Title := "Search Character"
	if Character != nil {
		Title = fmt.Sprintf("%v's Profile", Character.Name)
	}

	ExecuteTemplate(Context.Writer, "character_profile.tmpl",
		CharacterTmplData{
			Common: CommonTmplData{
				Title:     Title,
				AccountID: Context.AccountID,
			},
			Character: Character,
		})
}

func RenderKillStatisticsList(Context *THttpRequestContext) {
	ExecuteTemplate(Context.Writer, "killstatistics_list.tmpl",
		WorldListTmplData{
			Common: CommonTmplData{
				Title:     "Kill Statistics",
				AccountID: Context.AccountID,
			},
			Worlds: GetWorlds(),
		})
}

func RenderKillStatistics(Context *THttpRequestContext, WorldName string) {
	ExecuteTemplate(Context.Writer, "killstatistics.tmpl",
		KillStatisticsTmplData{
			Common: CommonTmplData{
				Title:     fmt.Sprintf("Kill Statistics - %v", WorldName),
				AccountID: Context.AccountID,
			},
			World:          GetWorld(WorldName),
			KillStatistics: GetKillStatistics(WorldName),
		})
}

func RenderWorldList(Context *THttpRequestContext) {
	ExecuteTemplate(Context.Writer, "world_list.tmpl",
		WorldListTmplData{
			Common: CommonTmplData{
				Title:     "Worlds",
				AccountID: Context.AccountID,
			},
			Worlds: GetWorlds(),
		})
}

func RenderWorldInfo(Context *THttpRequestContext, WorldName string) {
	ExecuteTemplate(Context.Writer, "world_info.tmpl",
		WorldTmplData{
			Common: CommonTmplData{
				Title:     "Worlds",
				AccountID: Context.AccountID,
			},
			World:            GetWorld(WorldName),
			OnlineCharacters: GetOnlineCharacters(WorldName),
		})
}
