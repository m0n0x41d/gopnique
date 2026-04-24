package operators

import (
	"context"
	"errors"
	"strings"

	"github.com/ivanzakutnii/error-tracker/internal/domain"
	"github.com/ivanzakutnii/error-tracker/internal/kernel/result"
)

type Access interface {
	IsBootstrapped(ctx context.Context) result.Result[bool]
	BootstrapOperator(ctx context.Context, command BootstrapCommand) result.Result[BootstrapResult]
	Login(ctx context.Context, command LoginCommand) result.Result[LoginResult]
	ResolveSession(ctx context.Context, token SessionToken) result.Result[OperatorSession]
	DeleteSession(ctx context.Context, token SessionToken) result.Result[struct{}]
}

type BootstrapCommand struct {
	PublicURL        string
	OrganizationName string
	ProjectName      string
	Email            string
	Password         string
}

type BootstrapResult struct {
	DSN     string
	Session SessionToken
}

type LoginCommand struct {
	Email    string
	Password string
}

type LoginResult struct {
	Session SessionToken
}

type OperatorSession struct {
	OperatorID       string
	Email            string
	OrganizationID   domain.OrganizationID
	ProjectID        domain.ProjectID
	OrganizationRole string
	ProjectRole      string
}

type SessionToken struct {
	value string
}

func NewSessionToken(input string) (SessionToken, error) {
	value := strings.TrimSpace(input)
	if len(value) < 32 {
		return SessionToken{}, errors.New("session token is invalid")
	}

	return SessionToken{value: value}, nil
}

func (token SessionToken) String() string {
	return token.value
}
