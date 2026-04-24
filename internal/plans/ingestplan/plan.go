package ingestplan

import (
	"errors"

	"github.com/ivanzakutnii/error-tracker/internal/domain"
	"github.com/ivanzakutnii/error-tracker/internal/kernel/result"
)

type AcceptedEvent struct {
	event       domain.CanonicalEvent
	fingerprint domain.Fingerprint
}

type IssuePlan struct {
	event AcceptedEvent
}

func NewAcceptedEvent(event domain.CanonicalEvent, fingerprint domain.Fingerprint) result.Result[AcceptedEvent] {
	if fingerprint.Value() == "" {
		return result.Err[AcceptedEvent](errors.New("fingerprint is required"))
	}

	return result.Ok(AcceptedEvent{
		event:       event,
		fingerprint: fingerprint,
	})
}

func NewIssuePlan(event AcceptedEvent) result.Result[IssuePlan] {
	if !event.event.CreatesIssue() {
		return result.Err[IssuePlan](errors.New("event does not create issue"))
	}

	return result.Ok(IssuePlan{event: event})
}

func (event AcceptedEvent) Event() domain.CanonicalEvent {
	return event.event
}

func (event AcceptedEvent) Fingerprint() domain.Fingerprint {
	return event.fingerprint
}

func (plan IssuePlan) Event() AcceptedEvent {
	return plan.event
}
