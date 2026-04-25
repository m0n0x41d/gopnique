package email

import (
	"context"
	"errors"
	"net"
	"net/mail"
	"net/smtp"
	"strings"
	"time"

	"github.com/ivanzakutnii/error-tracker/internal/app/notifications"
	"github.com/ivanzakutnii/error-tracker/internal/kernel/result"
)

type Mailer interface {
	SendMail(addr string, auth smtp.Auth, from string, to []string, msg []byte) error
}

type SMTPMailer struct{}

type SenderConfig struct {
	Addr     string
	Username string
	Password string
	From     string
}

type Sender struct {
	mailer   Mailer
	addr     string
	username string
	password string
	from     string
}

func NewSender(
	mailer Mailer,
	cfg SenderConfig,
) (Sender, error) {
	if mailer == nil {
		mailer = SMTPMailer{}
	}

	addr := strings.TrimSpace(cfg.Addr)
	if addr == "" {
		return Sender{}, errors.New("smtp addr is required")
	}

	_, _, splitErr := net.SplitHostPort(addr)
	if splitErr != nil {
		return Sender{}, errors.New("smtp addr must be host:port")
	}

	from := strings.TrimSpace(cfg.From)
	if from == "" {
		return Sender{}, errors.New("smtp from is required")
	}

	parsedFrom, fromErr := mail.ParseAddress(from)
	if fromErr != nil || parsedFrom.Address != from {
		return Sender{}, errors.New("smtp from must be an email address")
	}

	return Sender{
		mailer:   mailer,
		addr:     addr,
		username: strings.TrimSpace(cfg.Username),
		password: cfg.Password,
		from:     from,
	}, nil
}

func (SMTPMailer) SendMail(
	addr string,
	auth smtp.Auth,
	from string,
	to []string,
	msg []byte,
) error {
	return smtp.SendMail(addr, auth, from, to, msg)
}

func (sender Sender) SendEmail(
	ctx context.Context,
	message notifications.EmailMessage,
) result.Result[notifications.EmailSendReceipt] {
	select {
	case <-ctx.Done():
		return result.Err[notifications.EmailSendReceipt](ctx.Err())
	default:
	}

	messageIDResult := sender.messageID(message)
	messageID, messageIDErr := messageIDResult.Value()
	if messageIDErr != nil {
		return result.Err[notifications.EmailSendReceipt](messageIDErr)
	}

	bodyResult := sender.messageBody(message, messageID, time.Now().UTC())
	body, bodyErr := bodyResult.Value()
	if bodyErr != nil {
		return result.Err[notifications.EmailSendReceipt](bodyErr)
	}

	sendErr := sender.mailer.SendMail(
		sender.addr,
		sender.auth(),
		sender.from,
		[]string{message.To().String()},
		body,
	)
	if sendErr != nil {
		return result.Err[notifications.EmailSendReceipt](sendErr)
	}

	return result.Ok(notifications.NewEmailSendReceipt(messageID))
}

func (sender Sender) auth() smtp.Auth {
	if sender.username == "" {
		return nil
	}

	host, _, splitErr := net.SplitHostPort(sender.addr)
	if splitErr != nil {
		return nil
	}

	return smtp.PlainAuth("", sender.username, sender.password, host)
}

func (sender Sender) messageID(
	message notifications.EmailMessage,
) result.Result[string] {
	parsedFrom, fromErr := mail.ParseAddress(sender.from)
	if fromErr != nil {
		return result.Err[string](fromErr)
	}

	parts := strings.Split(parsedFrom.Address, "@")
	if len(parts) != 2 || parts[1] == "" {
		return result.Err[string](errors.New("smtp from domain is required"))
	}

	return result.Ok("<" + message.IntentID().String() + "@" + parts[1] + ">")
}

func (sender Sender) messageBody(
	message notifications.EmailMessage,
	messageID string,
	now time.Time,
) result.Result[[]byte] {
	headers := []string{
		"From: " + sender.from,
		"To: " + message.To().String(),
		"Subject: " + message.Subject().String(),
		"Message-ID: " + messageID,
		"Date: " + now.Format(time.RFC1123Z),
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=utf-8",
		"Content-Transfer-Encoding: 8bit",
		"",
		message.Body().String(),
	}

	body := strings.Join(headers, "\r\n")
	return result.Ok([]byte(body))
}
