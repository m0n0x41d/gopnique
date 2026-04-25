package ratelimit

import (
	"context"
	"errors"
	"time"

	"github.com/ivanzakutnii/error-tracker/internal/domain"
	"github.com/ivanzakutnii/error-tracker/internal/kernel/result"
)

type Checker interface {
	CheckRateLimit(ctx context.Context, command Command) result.Result[Decision]
}

type Scope struct {
	OrganizationID domain.OrganizationID
	ProjectID      domain.ProjectID
}

type Command struct {
	Scope     Scope
	PublicKey domain.ProjectPublicKey
	Now       time.Time
}

type Decision struct {
	allowed    bool
	reason     string
	retryAfter time.Duration
}

func Check(
	ctx context.Context,
	checker Checker,
	command Command,
) result.Result[Decision] {
	if checker == nil {
		return result.Err[Decision](errors.New("rate limit checker is required"))
	}

	commandResult := NormalizeCommand(command)
	normalizedCommand, commandErr := commandResult.Value()
	if commandErr != nil {
		return result.Err[Decision](commandErr)
	}

	return checker.CheckRateLimit(ctx, normalizedCommand)
}

func NormalizeCommand(command Command) result.Result[Command] {
	scopeErr := requireScope(command.Scope)
	if scopeErr != nil {
		return result.Err[Command](scopeErr)
	}

	if command.PublicKey.String() == "" {
		return result.Err[Command](errors.New("rate limit public key is required"))
	}

	if command.Now.IsZero() {
		return result.Err[Command](errors.New("rate limit reference time is required"))
	}

	command.Now = command.Now.UTC()

	return result.Ok(command)
}

func NewAllowed() Decision {
	return Decision{allowed: true}
}

func NewRejected(reason string, retryAfter time.Duration) Decision {
	if reason == "" {
		reason = "rate_limited"
	}

	if retryAfter < time.Second {
		retryAfter = time.Second
	}

	return Decision{
		reason:     reason,
		retryAfter: retryAfter,
	}
}

func (decision Decision) Allowed() bool {
	return decision.allowed
}

func (decision Decision) Reason() string {
	return decision.reason
}

func (decision Decision) RetryAfter() time.Duration {
	return decision.retryAfter
}

func requireScope(scope Scope) error {
	if scope.OrganizationID.String() == "" || scope.ProjectID.String() == "" {
		return errors.New("rate limit scope is required")
	}

	return nil
}
