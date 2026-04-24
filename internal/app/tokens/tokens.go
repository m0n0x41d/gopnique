package tokens

import (
	"context"
	"errors"
	"strings"

	"github.com/ivanzakutnii/error-tracker/internal/domain"
	"github.com/ivanzakutnii/error-tracker/internal/kernel/result"
)

type Manager interface {
	ListProjectTokens(ctx context.Context, query ProjectTokenQuery) result.Result[ProjectTokenView]
	CreateProjectToken(ctx context.Context, command CreateProjectTokenCommand) result.Result[CreateProjectTokenResult]
	RevokeProjectToken(ctx context.Context, command RevokeProjectTokenCommand) result.Result[ProjectTokenMutationResult]
	ResolveProjectToken(ctx context.Context, secret ProjectTokenSecret) result.Result[ProjectTokenAuth]
}

type Scope struct {
	OrganizationID domain.OrganizationID
	ProjectID      domain.ProjectID
}

type ProjectTokenScope string

const (
	ProjectTokenScopeRead  ProjectTokenScope = "project_read"
	ProjectTokenScopeAdmin ProjectTokenScope = "project_admin"
)

type ProjectTokenSecret struct {
	value string
}

type ProjectTokenQuery struct {
	Scope Scope
}

type CreateProjectTokenCommand struct {
	Scope      Scope
	ActorID    string
	Name       string
	TokenScope ProjectTokenScope
}

type RevokeProjectTokenCommand struct {
	Scope   Scope
	ActorID string
	TokenID domain.APITokenID
}

type ProjectTokenMutationResult struct {
	TokenID string
}

type CreateProjectTokenResult struct {
	TokenID      string
	OneTimeToken string
}

type ProjectTokenAuth struct {
	TokenID        domain.APITokenID
	OrganizationID domain.OrganizationID
	ProjectID      domain.ProjectID
	TokenScope     ProjectTokenScope
}

type ProjectTokenView struct {
	Tokens []ProjectTokenRow
}

type ProjectTokenRow struct {
	ID         string
	Name       string
	Prefix     string
	Scope      string
	Status     string
	CreatedAt  string
	LastUsedAt string
	RevokedAt  string
}

func ShowProjectTokens(
	ctx context.Context,
	manager Manager,
	query ProjectTokenQuery,
) result.Result[ProjectTokenView] {
	if manager == nil {
		return result.Err[ProjectTokenView](errors.New("token manager is required"))
	}

	scopeErr := requireScope(query.Scope)
	if scopeErr != nil {
		return result.Err[ProjectTokenView](scopeErr)
	}

	return manager.ListProjectTokens(ctx, query)
}

func CreateProjectToken(
	ctx context.Context,
	manager Manager,
	command CreateProjectTokenCommand,
) result.Result[CreateProjectTokenResult] {
	if manager == nil {
		return result.Err[CreateProjectTokenResult](errors.New("token manager is required"))
	}

	scopeErr := requireScope(command.Scope)
	if scopeErr != nil {
		return result.Err[CreateProjectTokenResult](scopeErr)
	}

	nameErr := requireName(command.Name)
	if nameErr != nil {
		return result.Err[CreateProjectTokenResult](nameErr)
	}

	actorErr := requireActor(command.ActorID)
	if actorErr != nil {
		return result.Err[CreateProjectTokenResult](actorErr)
	}

	if !command.TokenScope.Valid() {
		return result.Err[CreateProjectTokenResult](errors.New("token scope is invalid"))
	}

	return manager.CreateProjectToken(ctx, command)
}

func RevokeProjectToken(
	ctx context.Context,
	manager Manager,
	command RevokeProjectTokenCommand,
) result.Result[ProjectTokenMutationResult] {
	if manager == nil {
		return result.Err[ProjectTokenMutationResult](errors.New("token manager is required"))
	}

	scopeErr := requireScope(command.Scope)
	if scopeErr != nil {
		return result.Err[ProjectTokenMutationResult](scopeErr)
	}

	actorErr := requireActor(command.ActorID)
	if actorErr != nil {
		return result.Err[ProjectTokenMutationResult](actorErr)
	}

	if command.TokenID.String() == "" {
		return result.Err[ProjectTokenMutationResult](errors.New("token id is required"))
	}

	return manager.RevokeProjectToken(ctx, command)
}

func ResolveProjectToken(
	ctx context.Context,
	manager Manager,
	secret ProjectTokenSecret,
	required ProjectTokenScope,
) result.Result[ProjectTokenAuth] {
	if manager == nil {
		return result.Err[ProjectTokenAuth](errors.New("token manager is required"))
	}

	if !required.Valid() {
		return result.Err[ProjectTokenAuth](errors.New("required token scope is invalid"))
	}

	authResult := manager.ResolveProjectToken(ctx, secret)
	auth, authErr := authResult.Value()
	if authErr != nil {
		return result.Err[ProjectTokenAuth](authErr)
	}

	if !auth.TokenScope.Allows(required) {
		return result.Err[ProjectTokenAuth](errors.New("token scope denied"))
	}

	return result.Ok(auth)
}

func NewProjectTokenSecret(input string) (ProjectTokenSecret, error) {
	value := strings.TrimSpace(input)
	if !strings.HasPrefix(value, "etp_") {
		return ProjectTokenSecret{}, errors.New("api token prefix is invalid")
	}

	if len(value) < 36 {
		return ProjectTokenSecret{}, errors.New("api token is too short")
	}

	return ProjectTokenSecret{value: value}, nil
}

func (secret ProjectTokenSecret) String() string {
	return secret.value
}

func (scope ProjectTokenScope) Valid() bool {
	return scope == ProjectTokenScopeRead ||
		scope == ProjectTokenScopeAdmin
}

func (scope ProjectTokenScope) Allows(required ProjectTokenScope) bool {
	if scope == ProjectTokenScopeAdmin {
		return true
	}

	return scope == required
}

func ParseProjectTokenScope(input string) (ProjectTokenScope, error) {
	scope := ProjectTokenScope(strings.TrimSpace(input))
	if !scope.Valid() {
		return "", errors.New("token scope is invalid")
	}

	return scope, nil
}

func requireScope(scope Scope) error {
	if scope.OrganizationID.String() == "" || scope.ProjectID.String() == "" {
		return errors.New("token scope is required")
	}

	return nil
}

func requireName(input string) error {
	value := strings.TrimSpace(input)
	if value == "" {
		return errors.New("token name is required")
	}

	if len(value) > 80 {
		return errors.New("token name is too long")
	}

	return nil
}

func requireActor(input string) error {
	value := strings.TrimSpace(input)
	if value == "" {
		return errors.New("token actor is required")
	}

	return nil
}
