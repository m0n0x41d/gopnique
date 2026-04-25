package notifyplan

import (
	"errors"
	"fmt"
	"strings"

	"github.com/ivanzakutnii/error-tracker/internal/domain"
	"github.com/ivanzakutnii/error-tracker/internal/kernel/result"
)

type IssueOpened struct {
	event        domain.CanonicalEvent
	issueID      domain.IssueID
	issueShortID int64
}

type EmailIssueOpened struct {
	subject domain.NotificationSubject
	body    domain.NotificationText
}

func NewIssueOpened(
	event domain.CanonicalEvent,
	issueID domain.IssueID,
	issueShortID int64,
) result.Result[IssueOpened] {
	if !event.CreatesIssue() {
		return result.Err[IssueOpened](errors.New("issue event is required"))
	}

	if issueID.String() == "" {
		return result.Err[IssueOpened](errors.New("issue id is required"))
	}

	if issueShortID < 1 {
		return result.Err[IssueOpened](errors.New("issue short id must be positive"))
	}

	return result.Ok(IssueOpened{
		event:        event,
		issueID:      issueID,
		issueShortID: issueShortID,
	})
}

func TelegramIssueOpenedText(
	opened IssueOpened,
	publicURL string,
) result.Result[domain.NotificationText] {
	baseURL := strings.TrimRight(strings.TrimSpace(publicURL), "/")
	if baseURL == "" {
		return result.Err[domain.NotificationText](errors.New("public url is required"))
	}

	text := fmt.Sprintf(
		"New issue #%d\n%s\nLevel: %s\nEvent: %s\n%s/issues/%s",
		opened.issueShortID,
		opened.event.Title().String(),
		opened.event.Level(),
		opened.event.EventID().String(),
		baseURL,
		opened.issueID.String(),
	)

	notificationText, textErr := domain.NewNotificationText(text)
	if textErr != nil {
		return result.Err[domain.NotificationText](textErr)
	}

	return result.Ok(notificationText)
}

func EmailIssueOpenedMessage(
	opened IssueOpened,
	publicURL string,
) result.Result[EmailIssueOpened] {
	baseURL := strings.TrimRight(strings.TrimSpace(publicURL), "/")
	if baseURL == "" {
		return result.Err[EmailIssueOpened](errors.New("public url is required"))
	}

	subjectResult := emailIssueOpenedSubject(opened)
	subject, subjectErr := subjectResult.Value()
	if subjectErr != nil {
		return result.Err[EmailIssueOpened](subjectErr)
	}

	body := fmt.Sprintf(
		"New issue #%d\n\nTitle: %s\nLevel: %s\nPlatform: %s\nEvent: %s\nIssue: %s/issues/%s",
		opened.issueShortID,
		opened.event.Title().String(),
		opened.event.Level(),
		opened.event.Platform(),
		opened.event.EventID().String(),
		baseURL,
		opened.issueID.String(),
	)

	notificationText, textErr := domain.NewNotificationText(body)
	if textErr != nil {
		return result.Err[EmailIssueOpened](textErr)
	}

	return result.Ok(EmailIssueOpened{
		subject: subject,
		body:    notificationText,
	})
}

func emailIssueOpenedSubject(opened IssueOpened) result.Result[domain.NotificationSubject] {
	title := trimSubjectTitle(opened.event.Title().String(), 160)
	subject := fmt.Sprintf("New issue #%d: %s", opened.issueShortID, title)
	notificationSubject, subjectErr := domain.NewNotificationSubject(subject)
	if subjectErr != nil {
		return result.Err[domain.NotificationSubject](subjectErr)
	}

	return result.Ok(notificationSubject)
}

func trimSubjectTitle(input string, limit int) string {
	runes := []rune(strings.TrimSpace(input))
	if len(runes) <= limit {
		return string(runes)
	}

	return string(runes[:limit])
}

func (opened IssueOpened) Event() domain.CanonicalEvent {
	return opened.event
}

func (opened IssueOpened) IssueID() domain.IssueID {
	return opened.issueID
}

func (opened IssueOpened) IssueShortID() int64 {
	return opened.issueShortID
}

func (message EmailIssueOpened) Subject() domain.NotificationSubject {
	return message.subject
}

func (message EmailIssueOpened) Body() domain.NotificationText {
	return message.body
}
