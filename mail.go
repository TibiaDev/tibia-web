package main

import (
	"fmt"
	"net/smtp"
	"strings"
)

var (
	g_SmtpAuth smtp.Auth
)

func InitMail() bool {
	// TODO(fusion): I'm not entirely sure we can safely reuse smtp.Auth across
	// many threads. We might need to build a new auth struct everytime we need
	// to send an e-mail.
	g_SmtpAuth = smtp.PlainAuth("", g_SmtpUser, g_SmtpPassword, g_SmtpHost)
	return true
}

func ExitMail() {
	// no-op
}

func BuildMailMessage(From, To, Subject, Body string) string {
	Message := strings.Builder{}
	Message.WriteString("MIME-Version: 1.0\r\n")
	Message.WriteString("Content-Type: text/html; charset=utf-8\r\n")
	if From != "" {
		fmt.Fprintf(&Message, "From: %v\r\n", From)
	}
	if To != "" {
		fmt.Fprintf(&Message, "To: %v\r\n", To)
	}
	fmt.Fprintf(&Message, "Subject: %v\r\n", Subject)
	fmt.Fprintf(&Message, "\r\n%v\r\n", Body)
	return Message.String()
}

func SendMail(To, Subject, Body string) error {
	Message := BuildMailMessage(g_SmtpSender, To, Subject, Body)
	return smtp.SendMail(JoinHostPort(g_SmtpHost, g_SmtpPort),
		g_SmtpAuth, g_SmtpSender, []string{To}, []byte(Message))
}
