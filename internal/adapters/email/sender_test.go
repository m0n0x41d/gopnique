package email

import (
	"context"
	"net/smtp"
	"strings"
	"testing"

	"github.com/ivanzakutnii/error-tracker/internal/app/notifications"
	"github.com/ivanzakutnii/error-tracker/internal/domain"
)

func TestSenderUsesSMTPMailerWithIssueMessage(t *testing.T) {
	mailer := &fakeMailer{}
	sender, senderErr := NewSender(
		mailer,
		SenderConfig{
			Addr: "127.0.0.1:2525",
			From: "alerts@example.test",
		},
	)
	if senderErr != nil {
		t.Fatalf("sender: %v", senderErr)
	}

	sendResult := sender.SendEmail(context.Background(), emailMessage(t))
	receipt, sendErr := sendResult.Value()
	if sendErr != nil {
		t.Fatalf("send: %v", sendErr)
	}

	if receipt.ProviderMessageID() != "<44444444-4444-4444-a444-444444444444@example.test>" {
		t.Fatalf("unexpected receipt: %s", receipt.ProviderMessageID())
	}

	if mailer.addr != "127.0.0.1:2525" || mailer.from != "alerts@example.test" {
		t.Fatalf("unexpected envelope: %#v", mailer)
	}

	if len(mailer.to) != 1 || mailer.to[0] != "ops@example.test" {
		t.Fatalf("unexpected recipients: %#v", mailer.to)
	}

	body := string(mailer.msg)
	for _, expected := range []string{
		"From: alerts@example.test",
		"To: ops@example.test",
		"Subject: New issue #7: panic",
		"Message-ID: <44444444-4444-4444-a444-444444444444@example.test>",
		"body line",
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("expected message to contain %q: %s", expected, body)
		}
	}
}

type fakeMailer struct {
	addr string
	auth smtp.Auth
	from string
	to   []string
	msg  []byte
}

func (mailer *fakeMailer) SendMail(
	addr string,
	auth smtp.Auth,
	from string,
	to []string,
	msg []byte,
) error {
	mailer.addr = addr
	mailer.auth = auth
	mailer.from = from
	mailer.to = append([]string{}, to...)
	mailer.msg = append([]byte{}, msg...)

	return nil
}

func emailMessage(t *testing.T) notifications.EmailMessage {
	t.Helper()

	intentID := mustID(t, domain.NewNotificationIntentID, "44444444-4444-4444-a444-444444444444")
	address := emailAddress(t, "ops@example.test")
	subject := notificationSubject(t, "New issue #7: panic")
	body := notificationText(t, "body line")

	return notifications.NewEmailMessage(
		intentID,
		address,
		subject,
		body,
	)
}

func emailAddress(t *testing.T, input string) domain.EmailAddress {
	t.Helper()

	address, addressErr := domain.NewEmailAddress(input)
	if addressErr != nil {
		t.Fatalf("email address: %v", addressErr)
	}

	return address
}

func notificationSubject(t *testing.T, input string) domain.NotificationSubject {
	t.Helper()

	subject, subjectErr := domain.NewNotificationSubject(input)
	if subjectErr != nil {
		t.Fatalf("subject: %v", subjectErr)
	}

	return subject
}

func notificationText(t *testing.T, input string) domain.NotificationText {
	t.Helper()

	text, textErr := domain.NewNotificationText(input)
	if textErr != nil {
		t.Fatalf("text: %v", textErr)
	}

	return text
}

func mustID[T any](t *testing.T, constructor func(string) (T, error), input string) T {
	t.Helper()

	id, idErr := constructor(input)
	if idErr != nil {
		t.Fatalf("id: %v", idErr)
	}

	return id
}
