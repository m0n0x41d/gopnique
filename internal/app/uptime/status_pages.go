package uptime

import (
	"context"
	"errors"

	"github.com/ivanzakutnii/error-tracker/internal/domain"
	"github.com/ivanzakutnii/error-tracker/internal/kernel/result"
)

func CreateStatusPage(
	ctx context.Context,
	manager Manager,
	command CreateStatusPageCommand,
) result.Result[StatusPageMutationResult] {
	if manager == nil {
		return result.Err[StatusPageMutationResult](errors.New("uptime manager is required"))
	}

	scopeErr := requireScope(command.Scope)
	if scopeErr != nil {
		return result.Err[StatusPageMutationResult](scopeErr)
	}

	if command.ActorID == "" {
		return result.Err[StatusPageMutationResult](errors.New("status page actor is required"))
	}

	normalizedResult := statusPageCommand(command)
	normalized, normalizedErr := normalizedResult.Value()
	if normalizedErr != nil {
		return result.Err[StatusPageMutationResult](normalizedErr)
	}

	return manager.CreateStatusPage(ctx, normalized)
}

func ShowPrivateStatusPage(
	ctx context.Context,
	manager Manager,
	query PrivateStatusPageQuery,
) result.Result[StatusPageView] {
	if manager == nil {
		return result.Err[StatusPageView](errors.New("uptime manager is required"))
	}

	scopeErr := requireScope(query.Scope)
	if scopeErr != nil {
		return result.Err[StatusPageView](scopeErr)
	}

	pageID, pageIDErr := domain.NewStatusPageID(query.PageID)
	if pageIDErr != nil {
		return result.Err[StatusPageView](pageIDErr)
	}

	normalized := PrivateStatusPageQuery{
		Scope:  query.Scope,
		PageID: pageID.String(),
	}

	return manager.ShowPrivateStatusPage(ctx, normalized)
}

func ShowPublicStatusPage(
	ctx context.Context,
	manager Manager,
	query PublicStatusPageQuery,
) result.Result[StatusPageView] {
	if manager == nil {
		return result.Err[StatusPageView](errors.New("uptime manager is required"))
	}

	token, tokenErr := domain.NewStatusPageToken(query.Token)
	if tokenErr != nil {
		return result.Err[StatusPageView](tokenErr)
	}

	return manager.ShowPublicStatusPage(
		ctx,
		PublicStatusPageQuery{Token: token.String()},
	)
}

func statusPageCommand(
	command CreateStatusPageCommand,
) result.Result[CreateStatusPageCommand] {
	name, nameErr := domain.NewStatusPageName(command.Name)
	if nameErr != nil {
		return result.Err[CreateStatusPageCommand](nameErr)
	}

	visibility, visibilityErr := domain.ParseStatusPageVisibility(command.Visibility)
	if visibilityErr != nil {
		return result.Err[CreateStatusPageCommand](visibilityErr)
	}

	return result.Ok(CreateStatusPageCommand{
		Scope:      command.Scope,
		ActorID:    command.ActorID,
		Name:       name.String(),
		Visibility: string(visibility),
	})
}
