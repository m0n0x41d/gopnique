//go:build integration

package postgres

import (
	"context"
	"fmt"
	"net/netip"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	auditapp "github.com/ivanzakutnii/error-tracker/internal/app/audit"
	"github.com/ivanzakutnii/error-tracker/internal/app/ingest"
	issueapp "github.com/ivanzakutnii/error-tracker/internal/app/issues"
	memberapp "github.com/ivanzakutnii/error-tracker/internal/app/members"
	"github.com/ivanzakutnii/error-tracker/internal/app/notifications"
	"github.com/ivanzakutnii/error-tracker/internal/app/operators"
	projectapp "github.com/ivanzakutnii/error-tracker/internal/app/projects"
	settingsapp "github.com/ivanzakutnii/error-tracker/internal/app/settings"
	statsapp "github.com/ivanzakutnii/error-tracker/internal/app/stats"
	tokenapp "github.com/ivanzakutnii/error-tracker/internal/app/tokens"
	userreportapp "github.com/ivanzakutnii/error-tracker/internal/app/userreports"
	"github.com/ivanzakutnii/error-tracker/internal/domain"
	"github.com/ivanzakutnii/error-tracker/internal/kernel/result"
)

func TestPostgresRepositoryContract(t *testing.T) {
	ctx := context.Background()
	adminURL := repositoryAdminURL(t)
	databaseURL := createRepositoryTestDatabase(t, ctx, adminURL)
	store, storeErr := NewStore(ctx, databaseURL)
	if storeErr != nil {
		t.Fatalf("store: %v", storeErr)
	}
	defer store.Close()

	migrationResult, migrationErr := store.ApplyMigrations(ctx)
	if migrationErr != nil {
		t.Fatalf("migrate: %v", migrationErr)
	}
	if len(migrationResult.Applied) != 26 {
		t.Fatalf("expected 26 migrations, got %d", len(migrationResult.Applied))
	}

	bootstrap, bootstrapErr := store.Bootstrap(ctx, BootstrapInput{
		PublicURL:        "http://example.test",
		OrganizationName: "Repository Org",
		ProjectName:      "Repository API",
		OperatorEmail:    "operator@example.test",
		OperatorPassword: "correct-horse-battery-staple",
	})
	if bootstrapErr != nil {
		t.Fatalf("bootstrap: %v", bootstrapErr)
	}

	ref := mustRepositoryValue(t, domain.NewProjectRef, bootstrap.ProjectRef)
	publicKey := mustRepositoryValue(t, domain.NewProjectPublicKey, bootstrap.PublicKey)
	authResult := store.ResolveProjectKey(ctx, ref, publicKey)
	auth, authErr := authResult.Value()
	if authErr != nil {
		t.Fatalf("resolve project key: %v", authErr)
	}

	projectViewResult := projectapp.ShowCurrentProject(
		ctx,
		store,
		projectapp.ProjectQuery{
			Scope: projectapp.Scope{
				OrganizationID: auth.OrganizationID(),
				ProjectID:      auth.ProjectID(),
			},
			PublicURL: "http://example.test",
		},
	)
	projectView, projectViewErr := projectViewResult.Value()
	if projectViewErr != nil {
		t.Fatalf("project view: %v", projectViewErr)
	}
	if projectView.DSN != bootstrap.DSN {
		t.Fatalf("unexpected dsn: %s != %s", projectView.DSN, bootstrap.DSN)
	}

	membersResult := memberapp.Show(
		ctx,
		store,
		memberapp.Query{
			Scope: memberapp.Scope{
				OrganizationID: auth.OrganizationID(),
				ProjectID:      auth.ProjectID(),
			},
		},
	)
	members, membersErr := membersResult.Value()
	if membersErr != nil {
		t.Fatalf("members view: %v", membersErr)
	}
	if len(members.Operators) != 1 || len(members.Teams) != 1 {
		t.Fatalf("unexpected members view: %#v", members)
	}
	if members.Teams[0].Name != "Default team" || members.Teams[0].MemberCount != 1 {
		t.Fatalf("unexpected default team: %#v", members.Teams[0])
	}

	loginResult := store.Login(ctx, operators.LoginCommand{
		Email:    "operator@example.test",
		Password: "correct-horse-battery-staple",
	})
	login, loginErr := loginResult.Value()
	if loginErr != nil {
		t.Fatalf("login: %v", loginErr)
	}

	sessionResult := store.ResolveSession(ctx, login.Session)
	session, sessionErr := sessionResult.Value()
	if sessionErr != nil {
		t.Fatalf("session: %v", sessionErr)
	}

	tokenScope := tokenapp.Scope{
		OrganizationID: auth.OrganizationID(),
		ProjectID:      auth.ProjectID(),
	}
	createTokenResult := tokenapp.CreateProjectToken(
		ctx,
		store,
		tokenapp.CreateProjectTokenCommand{
			Scope:      tokenScope,
			ActorID:    session.OperatorID,
			Name:       "repo-api",
			TokenScope: tokenapp.ProjectTokenScopeRead,
		},
	)
	createdToken, createTokenErr := createTokenResult.Value()
	if createTokenErr != nil {
		t.Fatalf("create api token: %v", createTokenErr)
	}
	if !strings.HasPrefix(createdToken.OneTimeToken, "etp_") {
		t.Fatalf("unexpected api token secret: %s", createdToken.OneTimeToken)
	}

	tokensResult := tokenapp.ShowProjectTokens(
		ctx,
		store,
		tokenapp.ProjectTokenQuery{Scope: tokenScope},
	)
	tokens, tokensErr := tokensResult.Value()
	if tokensErr != nil {
		t.Fatalf("api token list: %v", tokensErr)
	}
	if len(tokens.Tokens) != 1 || tokens.Tokens[0].Name != "repo-api" {
		t.Fatalf("unexpected api token list: %#v", tokens)
	}

	secret, secretErr := tokenapp.NewProjectTokenSecret(createdToken.OneTimeToken)
	if secretErr != nil {
		t.Fatalf("api token secret: %v", secretErr)
	}

	resolveTokenResult := tokenapp.ResolveProjectToken(ctx, store, secret, tokenapp.ProjectTokenScopeRead)
	tokenAuth, resolveTokenErr := resolveTokenResult.Value()
	if resolveTokenErr != nil {
		t.Fatalf("resolve api token: %v", resolveTokenErr)
	}
	if tokenAuth.ProjectID.String() != auth.ProjectID().String() {
		t.Fatalf("unexpected api token auth: %#v", tokenAuth)
	}

	revokeTokenID := mustRepositoryValue(t, domain.NewAPITokenID, createdToken.TokenID)
	revokeTokenResult := tokenapp.RevokeProjectToken(
		ctx,
		store,
		tokenapp.RevokeProjectTokenCommand{
			Scope:   tokenScope,
			ActorID: session.OperatorID,
			TokenID: revokeTokenID,
		},
	)
	_, revokeTokenErr := revokeTokenResult.Value()
	if revokeTokenErr != nil {
		t.Fatalf("revoke api token: %v", revokeTokenErr)
	}

	revokedTokenResult := tokenapp.ResolveProjectToken(ctx, store, secret, tokenapp.ProjectTokenScopeRead)
	_, revokedTokenErr := revokedTokenResult.Value()
	if revokedTokenErr == nil {
		t.Fatal("expected revoked api token to fail")
	}

	scope := settingsapp.Scope{
		OrganizationID: auth.OrganizationID(),
		ProjectID:      auth.ProjectID(),
	}
	destinationResult := settingsapp.AddTelegramDestination(
		ctx,
		store,
		settingsapp.AddTelegramDestinationCommand{
			Scope:  scope,
			ChatID: "123456",
			Label:  "ops-telegram",
		},
	)
	destination, destinationErr := destinationResult.Value()
	if destinationErr != nil {
		t.Fatalf("telegram destination: %v", destinationErr)
	}

	webhookResult := settingsapp.AddWebhookDestination(
		ctx,
		repositoryResolver{"hooks.example.com": []netip.Addr{netip.MustParseAddr("93.184.216.34")}},
		store,
		settingsapp.AddWebhookDestinationCommand{
			Scope: scope,
			URL:   "https://hooks.example.com/error-tracker",
			Label: "ops-webhook",
		},
	)
	webhookDestination, webhookErr := webhookResult.Value()
	if webhookErr != nil {
		t.Fatalf("webhook destination: %v", webhookErr)
	}

	emailResult := settingsapp.AddEmailDestination(
		ctx,
		store,
		settingsapp.AddEmailDestinationCommand{
			Scope:   scope,
			Address: "ops@example.test",
			Label:   "ops-email",
		},
	)
	emailDestination, emailErr := emailResult.Value()
	if emailErr != nil {
		t.Fatalf("email destination: %v", emailErr)
	}

	discordResult := settingsapp.AddDiscordDestination(
		ctx,
		repositoryResolver{"discord.example.com": []netip.Addr{netip.MustParseAddr("93.184.216.34")}},
		store,
		settingsapp.AddDiscordDestinationCommand{
			Scope: scope,
			URL:   "https://discord.example.com/webhook",
			Label: "ops-discord",
		},
	)
	discordDestination, discordErr := discordResult.Value()
	if discordErr != nil {
		t.Fatalf("discord destination: %v", discordErr)
	}

	googleChatResult := settingsapp.AddGoogleChatDestination(
		ctx,
		repositoryResolver{"chat.example.com": []netip.Addr{netip.MustParseAddr("93.184.216.34")}},
		store,
		settingsapp.AddGoogleChatDestinationCommand{
			Scope: scope,
			URL:   "https://chat.example.com/webhook",
			Label: "ops-google-chat",
		},
	)
	googleChatDestination, googleChatErr := googleChatResult.Value()
	if googleChatErr != nil {
		t.Fatalf("google chat destination: %v", googleChatErr)
	}

	ntfyResult := settingsapp.AddNtfyDestination(
		ctx,
		repositoryResolver{"ntfy.example.com": []netip.Addr{netip.MustParseAddr("93.184.216.34")}},
		store,
		settingsapp.AddNtfyDestinationCommand{
			Scope: scope,
			URL:   "https://ntfy.example.com",
			Topic: "ops-alerts",
			Label: "ops-ntfy",
		},
	)
	ntfyDestination, ntfyErr := ntfyResult.Value()
	if ntfyErr != nil {
		t.Fatalf("ntfy destination: %v", ntfyErr)
	}

	teamsResult := settingsapp.AddTeamsDestination(
		ctx,
		repositoryResolver{"teams.example.com": []netip.Addr{netip.MustParseAddr("93.184.216.34")}},
		store,
		settingsapp.AddTeamsDestinationCommand{
			Scope: scope,
			URL:   "https://teams.example.com/webhook",
			Label: "ops-teams",
		},
	)
	teamsDestination, teamsErr := teamsResult.Value()
	if teamsErr != nil {
		t.Fatalf("microsoft teams destination: %v", teamsErr)
	}

	zulipResult := settingsapp.AddZulipDestination(
		ctx,
		repositoryResolver{"zulip.example.com": []netip.Addr{netip.MustParseAddr("93.184.216.34")}},
		store,
		settingsapp.AddZulipDestinationCommand{
			Scope:    scope,
			URL:      "https://zulip.example.com",
			BotEmail: "bot@example.test",
			APIKey:   "zulip-key",
			Stream:   "ops",
			Topic:    "alerts",
			Label:    "ops-zulip",
		},
	)
	zulipDestination, zulipErr := zulipResult.Value()
	if zulipErr != nil {
		t.Fatalf("zulip destination: %v", zulipErr)
	}

	alertResult := settingsapp.AddIssueOpenedTelegramAlert(
		ctx,
		store,
		settingsapp.AddIssueOpenedTelegramAlertCommand{
			Scope:         scope,
			DestinationID: destination.DestinationID,
			Name:          "Issue opened to Telegram",
		},
	)
	_, alertErr := alertResult.Value()
	if alertErr != nil {
		t.Fatalf("issue-opened alert: %v", alertErr)
	}

	webhookAlertResult := settingsapp.AddIssueOpenedAlert(
		ctx,
		store,
		settingsapp.AddIssueOpenedAlertCommand{
			Scope:         scope,
			Provider:      domain.AlertActionProviderWebhook,
			DestinationID: webhookDestination.DestinationID,
			Name:          "Issue opened to Webhook",
		},
	)
	_, webhookAlertErr := webhookAlertResult.Value()
	if webhookAlertErr != nil {
		t.Fatalf("issue-opened webhook alert: %v", webhookAlertErr)
	}

	emailAlertResult := settingsapp.AddIssueOpenedAlert(
		ctx,
		store,
		settingsapp.AddIssueOpenedAlertCommand{
			Scope:         scope,
			Provider:      domain.AlertActionProviderEmail,
			DestinationID: emailDestination.DestinationID,
			Name:          "Issue opened to Email",
		},
	)
	_, emailAlertErr := emailAlertResult.Value()
	if emailAlertErr != nil {
		t.Fatalf("issue-opened email alert: %v", emailAlertErr)
	}

	discordAlertResult := settingsapp.AddIssueOpenedAlert(
		ctx,
		store,
		settingsapp.AddIssueOpenedAlertCommand{
			Scope:         scope,
			Provider:      domain.AlertActionProviderDiscord,
			DestinationID: discordDestination.DestinationID,
			Name:          "Issue opened to Discord",
		},
	)
	_, discordAlertErr := discordAlertResult.Value()
	if discordAlertErr != nil {
		t.Fatalf("issue-opened discord alert: %v", discordAlertErr)
	}

	googleChatAlertResult := settingsapp.AddIssueOpenedAlert(
		ctx,
		store,
		settingsapp.AddIssueOpenedAlertCommand{
			Scope:         scope,
			Provider:      domain.AlertActionProviderGoogleChat,
			DestinationID: googleChatDestination.DestinationID,
			Name:          "Issue opened to Google Chat",
		},
	)
	_, googleChatAlertErr := googleChatAlertResult.Value()
	if googleChatAlertErr != nil {
		t.Fatalf("issue-opened google chat alert: %v", googleChatAlertErr)
	}

	ntfyAlertResult := settingsapp.AddIssueOpenedAlert(
		ctx,
		store,
		settingsapp.AddIssueOpenedAlertCommand{
			Scope:         scope,
			Provider:      domain.AlertActionProviderNtfy,
			DestinationID: ntfyDestination.DestinationID,
			Name:          "Issue opened to ntfy",
		},
	)
	_, ntfyAlertErr := ntfyAlertResult.Value()
	if ntfyAlertErr != nil {
		t.Fatalf("issue-opened ntfy alert: %v", ntfyAlertErr)
	}

	teamsAlertResult := settingsapp.AddIssueOpenedAlert(
		ctx,
		store,
		settingsapp.AddIssueOpenedAlertCommand{
			Scope:         scope,
			Provider:      domain.AlertActionProviderTeams,
			DestinationID: teamsDestination.DestinationID,
			Name:          "Issue opened to Microsoft Teams",
		},
	)
	_, teamsAlertErr := teamsAlertResult.Value()
	if teamsAlertErr != nil {
		t.Fatalf("issue-opened microsoft teams alert: %v", teamsAlertErr)
	}

	zulipAlertResult := settingsapp.AddIssueOpenedAlert(
		ctx,
		store,
		settingsapp.AddIssueOpenedAlertCommand{
			Scope:         scope,
			Provider:      domain.AlertActionProviderZulip,
			DestinationID: zulipDestination.DestinationID,
			Name:          "Issue opened to Zulip",
		},
	)
	_, zulipAlertErr := zulipAlertResult.Value()
	if zulipAlertErr != nil {
		t.Fatalf("issue-opened zulip alert: %v", zulipAlertErr)
	}

	settingsResult := settingsapp.ShowProjectSettings(ctx, store, settingsapp.ProjectSettingsQuery{Scope: scope})
	settings, settingsErr := settingsResult.Value()
	if settingsErr != nil {
		t.Fatalf("project settings: %v", settingsErr)
	}
	if len(settings.TelegramDestinations) != 1 ||
		len(settings.WebhookDestinations) != 1 ||
		len(settings.EmailDestinations) != 1 ||
		len(settings.DiscordDestinations) != 1 ||
		len(settings.GoogleChatDestinations) != 1 ||
		len(settings.NtfyDestinations) != 1 ||
		len(settings.TeamsDestinations) != 1 ||
		len(settings.ZulipDestinations) != 1 ||
		len(settings.IssueOpenedAlerts) != 8 {
		t.Fatalf("unexpected settings view: %#v", settings)
	}

	event := repositoryEvent(t, auth.OrganizationID(), auth.ProjectID(), "550e8400e29b41d4a716446655440000")
	receiptResult := ingest.IngestCanonicalEvent(ctx, ingest.NewIngestCommand(event), store)
	receipt, receiptErr := receiptResult.Value()
	if receiptErr != nil {
		t.Fatalf("ingest event: %v", receiptErr)
	}
	if receipt.Kind() != ingest.ReceiptAcceptedIssueEvent {
		t.Fatalf("unexpected receipt kind: %s", receipt.Kind())
	}

	secondEvent := repositoryEvent(t, auth.OrganizationID(), auth.ProjectID(), "560e8400e29b41d4a716446655440000")
	secondReceiptResult := ingest.IngestCanonicalEvent(ctx, ingest.NewIngestCommand(secondEvent), store)
	secondReceipt, secondReceiptErr := secondReceiptResult.Value()
	if secondReceiptErr != nil {
		t.Fatalf("ingest second event: %v", secondReceiptErr)
	}
	if secondReceipt.Kind() != ingest.ReceiptAcceptedIssueEvent {
		t.Fatalf("unexpected second receipt kind: %s", secondReceipt.Kind())
	}

	duplicateResult := ingest.IngestCanonicalEvent(ctx, ingest.NewIngestCommand(event), store)
	duplicate, duplicateErr := duplicateResult.Value()
	if duplicateErr != nil {
		t.Fatalf("ingest duplicate: %v", duplicateErr)
	}
	if duplicate.Kind() != ingest.ReceiptDuplicateEvent {
		t.Fatalf("unexpected duplicate receipt: %s", duplicate.Kind())
	}

	statsResult := statsapp.ShowProjectStats(
		ctx,
		store,
		statsapp.Query{
			Scope: statsapp.Scope{
				OrganizationID: auth.OrganizationID(),
				ProjectID:      auth.ProjectID(),
			},
			Period: statsapp.Period24h,
			Now:    time.Date(2026, 4, 24, 13, 30, 0, 0, time.UTC),
		},
	)
	statsView, statsErr := statsResult.Value()
	if statsErr != nil {
		t.Fatalf("project stats: %v", statsErr)
	}
	if statsView.TotalEvents != 2 || statsView.IssueEvents != 2 || statsView.TransactionEvents != 0 {
		t.Fatalf("unexpected event stats: %#v", statsView)
	}
	if statsView.MaxBucketEvents != 2 || len(statsView.Buckets) != 24 {
		t.Fatalf("unexpected bucket stats: %#v", statsView)
	}

	issueListResult := issueapp.List(
		ctx,
		store,
		issueapp.IssueListQuery{
			Scope: issueapp.Scope{
				OrganizationID: auth.OrganizationID(),
				ProjectID:      auth.ProjectID(),
			},
			Limit: 50,
		},
	)
	issueList, issueListErr := issueListResult.Value()
	if issueListErr != nil {
		t.Fatalf("issue list: %v", issueListErr)
	}
	if len(issueList.Items) != 1 || issueList.Items[0].Title != "RepositoryError: repository contract failure" {
		t.Fatalf("unexpected issue list: %#v", issueList)
	}
	if issueList.Items[0].EventCount != 2 {
		t.Fatalf("expected two grouped events, got %d", issueList.Items[0].EventCount)
	}

	issueID := mustRepositoryValue(t, domain.NewIssueID, issueList.Items[0].ID)
	reportResult := userreportapp.Submit(
		ctx,
		store,
		userreportapp.SubmitCommand{
			Scope: userreportapp.Scope{
				OrganizationID: auth.OrganizationID(),
				ProjectID:      auth.ProjectID(),
			},
			EventID:  event.EventID(),
			Name:     "Repo User",
			Email:    "repo-user@example.test",
			Comments: "Repository contract report",
		},
	)
	report, reportErr := reportResult.Value()
	if reportErr != nil {
		t.Fatalf("submit user report: %v", reportErr)
	}
	if report.ReportID == "" {
		t.Fatal("expected user report id")
	}

	reportsResult := userreportapp.ListForIssue(
		ctx,
		store,
		userreportapp.IssueReportsQuery{
			Scope: userreportapp.Scope{
				OrganizationID: auth.OrganizationID(),
				ProjectID:      auth.ProjectID(),
			},
			IssueID: issueID,
			Limit:   50,
		},
	)
	reports, reportsErr := reportsResult.Value()
	if reportsErr != nil {
		t.Fatalf("list user reports: %v", reportsErr)
	}
	if len(reports.Items) != 1 || reports.Items[0].Comments != "Repository contract report" {
		t.Fatalf("unexpected user reports: %#v", reports.Items)
	}

	reportedStatsResult := statsapp.ShowProjectStats(
		ctx,
		store,
		statsapp.Query{
			Scope: statsapp.Scope{
				OrganizationID: auth.OrganizationID(),
				ProjectID:      auth.ProjectID(),
			},
			Period: statsapp.Period24h,
			Now:    time.Date(2026, 4, 24, 13, 30, 0, 0, time.UTC),
		},
	)
	reportedStats, reportedStatsErr := reportedStatsResult.Value()
	if reportedStatsErr != nil {
		t.Fatalf("reported project stats: %v", reportedStatsErr)
	}
	if reportedStats.UserReports != 1 {
		t.Fatalf("unexpected user report stats: %#v", reportedStats)
	}

	commentResult := issueapp.AddComment(
		ctx,
		store,
		issueapp.AddCommentCommand{
			Scope: issueapp.Scope{
				OrganizationID: auth.OrganizationID(),
				ProjectID:      auth.ProjectID(),
			},
			IssueID: issueID,
			ActorID: session.OperatorID,
			Body:    "Repository contract comment",
		},
	)
	comment, commentErr := commentResult.Value()
	if commentErr != nil {
		t.Fatalf("add comment: %v", commentErr)
	}
	if comment.CommentID == "" {
		t.Fatal("expected comment id")
	}

	commentedDetailResult := issueapp.Detail(
		ctx,
		store,
		issueapp.IssueDetailQuery{
			Scope: issueapp.Scope{
				OrganizationID: auth.OrganizationID(),
				ProjectID:      auth.ProjectID(),
			},
			IssueID: issueID,
		},
	)
	commentedDetail, commentedDetailErr := commentedDetailResult.Value()
	if commentedDetailErr != nil {
		t.Fatalf("commented detail: %v", commentedDetailErr)
	}
	if len(commentedDetail.Comments) != 1 || commentedDetail.Comments[0].Body != "Repository contract comment" {
		t.Fatalf("unexpected comments: %#v", commentedDetail.Comments)
	}

	teamAssignee := assignmentOptionByKind(t, commentedDetail, "team")
	assignmentTarget, assignmentTargetErr := issueapp.ParseAssignmentTarget(teamAssignee.Value)
	if assignmentTargetErr != nil {
		t.Fatalf("assignment target: %v", assignmentTargetErr)
	}
	assignResult := issueapp.Assign(
		ctx,
		store,
		issueapp.AssignIssueCommand{
			Scope: issueapp.Scope{
				OrganizationID: auth.OrganizationID(),
				ProjectID:      auth.ProjectID(),
			},
			IssueID: issueID,
			ActorID: session.OperatorID,
			Target:  assignmentTarget,
		},
	)
	_, assignErr := assignResult.Value()
	if assignErr != nil {
		t.Fatalf("assign issue: %v", assignErr)
	}

	assignedDetailResult := issueapp.Detail(
		ctx,
		store,
		issueapp.IssueDetailQuery{
			Scope: issueapp.Scope{
				OrganizationID: auth.OrganizationID(),
				ProjectID:      auth.ProjectID(),
			},
			IssueID: issueID,
		},
	)
	assignedDetail, assignedDetailErr := assignedDetailResult.Value()
	if assignedDetailErr != nil {
		t.Fatalf("assigned detail: %v", assignedDetailErr)
	}
	if assignedDetail.Assignee != "Default team" {
		t.Fatalf("unexpected assignee: %#v", assignedDetail)
	}

	resolveResult := issueapp.TransitionStatus(
		ctx,
		store,
		issueapp.StatusTransitionCommand{
			Scope: issueapp.Scope{
				OrganizationID: auth.OrganizationID(),
				ProjectID:      auth.ProjectID(),
			},
			IssueID:      issueID,
			ActorID:      session.OperatorID,
			TargetStatus: issueapp.IssueStatusResolved,
			Reason:       "repository contract",
		},
	)
	_, resolveErr := resolveResult.Value()
	if resolveErr != nil {
		t.Fatalf("resolve issue: %v", resolveErr)
	}

	unresolvedAfterResolveResult := issueapp.List(
		ctx,
		store,
		issueapp.IssueListQuery{
			Scope: issueapp.Scope{
				OrganizationID: auth.OrganizationID(),
				ProjectID:      auth.ProjectID(),
			},
			Limit:  50,
			Status: issueapp.IssueStatusUnresolved,
		},
	)
	unresolvedAfterResolve, unresolvedAfterResolveErr := unresolvedAfterResolveResult.Value()
	if unresolvedAfterResolveErr != nil {
		t.Fatalf("unresolved issue list after resolve: %v", unresolvedAfterResolveErr)
	}
	if len(unresolvedAfterResolve.Items) != 0 {
		t.Fatalf("expected resolved issue to leave unresolved list: %#v", unresolvedAfterResolve)
	}

	resolvedListResult := issueapp.List(
		ctx,
		store,
		issueapp.IssueListQuery{
			Scope: issueapp.Scope{
				OrganizationID: auth.OrganizationID(),
				ProjectID:      auth.ProjectID(),
			},
			Limit:  50,
			Status: issueapp.IssueStatusResolved,
		},
	)
	resolvedList, resolvedListErr := resolvedListResult.Value()
	if resolvedListErr != nil {
		t.Fatalf("resolved issue list: %v", resolvedListErr)
	}
	if len(resolvedList.Items) != 1 || resolvedList.Items[0].Status != "resolved" {
		t.Fatalf("unexpected resolved issue list: %#v", resolvedList)
	}

	invalidTransitionResult := issueapp.TransitionStatus(
		ctx,
		store,
		issueapp.StatusTransitionCommand{
			Scope: issueapp.Scope{
				OrganizationID: auth.OrganizationID(),
				ProjectID:      auth.ProjectID(),
			},
			IssueID:      issueID,
			ActorID:      session.OperatorID,
			TargetStatus: issueapp.IssueStatusIgnored,
		},
	)
	_, invalidTransitionErr := invalidTransitionResult.Value()
	if invalidTransitionErr == nil {
		t.Fatal("expected resolved to ignored transition to fail")
	}

	reopenResult := issueapp.TransitionStatus(
		ctx,
		store,
		issueapp.StatusTransitionCommand{
			Scope: issueapp.Scope{
				OrganizationID: auth.OrganizationID(),
				ProjectID:      auth.ProjectID(),
			},
			IssueID:      issueID,
			ActorID:      session.OperatorID,
			TargetStatus: issueapp.IssueStatusUnresolved,
		},
	)
	_, reopenErr := reopenResult.Value()
	if reopenErr != nil {
		t.Fatalf("reopen issue: %v", reopenErr)
	}

	auditResult := auditapp.List(
		ctx,
		store,
		auditapp.Query{
			Scope: auditapp.Scope{
				OrganizationID: auth.OrganizationID(),
				ProjectID:      auth.ProjectID(),
			},
			Limit: 50,
		},
	)
	auditView, auditErr := auditResult.Value()
	if auditErr != nil {
		t.Fatalf("audit list: %v", auditErr)
	}
	auditActions := auditActionsByName(auditView)
	for _, action := range []string{
		"bootstrap",
		"api_token_created",
		"api_token_revoked",
		"issue_assigned",
		"issue_comment_created",
		"issue_status_changed",
	} {
		if !auditActions[action] {
			t.Fatalf("expected audit action %s in %#v", action, auditView)
		}
	}

	deliveries := claimTelegramConcurrently(t, ctx, store)
	if len(deliveries) != 1 {
		t.Fatalf("expected one telegram delivery, got %d", len(deliveries))
	}

	secondTelegramClaimResult := store.ClaimTelegramDeliveries(ctx, time.Now().UTC(), 10)
	secondTelegramClaim, secondTelegramClaimErr := secondTelegramClaimResult.Value()
	if secondTelegramClaimErr != nil {
		t.Fatalf("second telegram claim: %v", secondTelegramClaimErr)
	}
	if len(secondTelegramClaim) != 0 {
		t.Fatalf("expected telegram lease to hide claimed delivery, got %d", len(secondTelegramClaim))
	}

	webhookDeliveries := claimWebhookConcurrently(t, ctx, store)
	if len(webhookDeliveries) != 1 {
		t.Fatalf("expected one webhook delivery, got %d", len(webhookDeliveries))
	}

	secondWebhookClaimResult := store.ClaimWebhookDeliveries(ctx, time.Now().UTC(), 10)
	secondWebhookClaim, secondWebhookClaimErr := secondWebhookClaimResult.Value()
	if secondWebhookClaimErr != nil {
		t.Fatalf("second webhook claim: %v", secondWebhookClaimErr)
	}
	if len(secondWebhookClaim) != 0 {
		t.Fatalf("expected webhook lease to hide claimed delivery, got %d", len(secondWebhookClaim))
	}

	emailDeliveries := claimEmailConcurrently(t, ctx, store)
	if len(emailDeliveries) != 1 {
		t.Fatalf("expected one email delivery, got %d", len(emailDeliveries))
	}

	secondEmailClaimResult := store.ClaimEmailDeliveries(ctx, time.Now().UTC(), 10)
	secondEmailClaim, secondEmailClaimErr := secondEmailClaimResult.Value()
	if secondEmailClaimErr != nil {
		t.Fatalf("second email claim: %v", secondEmailClaimErr)
	}
	if len(secondEmailClaim) != 0 {
		t.Fatalf("expected email lease to hide claimed delivery, got %d", len(secondEmailClaim))
	}

	discordDeliveries := claimDiscordConcurrently(t, ctx, store)
	if len(discordDeliveries) != 1 {
		t.Fatalf("expected one discord delivery, got %d", len(discordDeliveries))
	}

	secondDiscordClaimResult := store.ClaimDiscordDeliveries(ctx, time.Now().UTC(), 10)
	secondDiscordClaim, secondDiscordClaimErr := secondDiscordClaimResult.Value()
	if secondDiscordClaimErr != nil {
		t.Fatalf("second discord claim: %v", secondDiscordClaimErr)
	}
	if len(secondDiscordClaim) != 0 {
		t.Fatalf("expected discord lease to hide claimed delivery, got %d", len(secondDiscordClaim))
	}

	googleChatDeliveries := claimGoogleChatConcurrently(t, ctx, store)
	if len(googleChatDeliveries) != 1 {
		t.Fatalf("expected one google chat delivery, got %d", len(googleChatDeliveries))
	}

	secondGoogleChatClaimResult := store.ClaimGoogleChatDeliveries(ctx, time.Now().UTC(), 10)
	secondGoogleChatClaim, secondGoogleChatClaimErr := secondGoogleChatClaimResult.Value()
	if secondGoogleChatClaimErr != nil {
		t.Fatalf("second google chat claim: %v", secondGoogleChatClaimErr)
	}
	if len(secondGoogleChatClaim) != 0 {
		t.Fatalf("expected google chat lease to hide claimed delivery, got %d", len(secondGoogleChatClaim))
	}

	ntfyDeliveries := claimNtfyConcurrently(t, ctx, store)
	if len(ntfyDeliveries) != 1 {
		t.Fatalf("expected one ntfy delivery, got %d", len(ntfyDeliveries))
	}

	secondNtfyClaimResult := store.ClaimNtfyDeliveries(ctx, time.Now().UTC(), 10)
	secondNtfyClaim, secondNtfyClaimErr := secondNtfyClaimResult.Value()
	if secondNtfyClaimErr != nil {
		t.Fatalf("second ntfy claim: %v", secondNtfyClaimErr)
	}
	if len(secondNtfyClaim) != 0 {
		t.Fatalf("expected ntfy lease to hide claimed delivery, got %d", len(secondNtfyClaim))
	}

	teamsDeliveries := claimTeamsConcurrently(t, ctx, store)
	if len(teamsDeliveries) != 1 {
		t.Fatalf("expected one microsoft teams delivery, got %d", len(teamsDeliveries))
	}

	secondTeamsClaimResult := store.ClaimTeamsDeliveries(ctx, time.Now().UTC(), 10)
	secondTeamsClaim, secondTeamsClaimErr := secondTeamsClaimResult.Value()
	if secondTeamsClaimErr != nil {
		t.Fatalf("second microsoft teams claim: %v", secondTeamsClaimErr)
	}
	if len(secondTeamsClaim) != 0 {
		t.Fatalf("expected microsoft teams lease to hide claimed delivery, got %d", len(secondTeamsClaim))
	}

	zulipDeliveries := claimZulipConcurrently(t, ctx, store)
	if len(zulipDeliveries) != 1 {
		t.Fatalf("expected one zulip delivery, got %d", len(zulipDeliveries))
	}

	secondZulipClaimResult := store.ClaimZulipDeliveries(ctx, time.Now().UTC(), 10)
	secondZulipClaim, secondZulipClaimErr := secondZulipClaimResult.Value()
	if secondZulipClaimErr != nil {
		t.Fatalf("second zulip claim: %v", secondZulipClaimErr)
	}
	if len(secondZulipClaim) != 0 {
		t.Fatalf("expected zulip lease to hide claimed delivery, got %d", len(secondZulipClaim))
	}

	markResult := store.MarkTelegramDelivered(
		ctx,
		deliveries[0].IntentID(),
		time.Now().UTC(),
		notifications.NewTelegramSendReceipt("provider-1"),
	)
	_, markErr := markResult.Value()
	if markErr != nil {
		t.Fatalf("mark delivered: %v", markErr)
	}

	markWebhookResult := store.MarkWebhookDelivered(
		ctx,
		webhookDeliveries[0].IntentID(),
		time.Now().UTC(),
		notifications.NewWebhookDeliveredReceipt(204),
	)
	_, markWebhookErr := markWebhookResult.Value()
	if markWebhookErr != nil {
		t.Fatalf("mark webhook delivered: %v", markWebhookErr)
	}

	markEmailResult := store.MarkEmailDelivered(
		ctx,
		emailDeliveries[0].IntentID(),
		time.Now().UTC(),
		notifications.NewEmailSendReceipt("<repo-email@example.test>"),
	)
	_, markEmailErr := markEmailResult.Value()
	if markEmailErr != nil {
		t.Fatalf("mark email delivered: %v", markEmailErr)
	}

	markDiscordResult := store.MarkDiscordDelivered(
		ctx,
		discordDeliveries[0].IntentID(),
		time.Now().UTC(),
		notifications.NewDiscordDeliveredReceipt(204),
	)
	_, markDiscordErr := markDiscordResult.Value()
	if markDiscordErr != nil {
		t.Fatalf("mark discord delivered: %v", markDiscordErr)
	}

	markGoogleChatResult := store.MarkGoogleChatDelivered(
		ctx,
		googleChatDeliveries[0].IntentID(),
		time.Now().UTC(),
		notifications.NewGoogleChatDeliveredReceipt(200),
	)
	_, markGoogleChatErr := markGoogleChatResult.Value()
	if markGoogleChatErr != nil {
		t.Fatalf("mark google chat delivered: %v", markGoogleChatErr)
	}

	markNtfyResult := store.MarkNtfyDelivered(
		ctx,
		ntfyDeliveries[0].IntentID(),
		time.Now().UTC(),
		notifications.NewNtfyDeliveredReceipt(200),
	)
	_, markNtfyErr := markNtfyResult.Value()
	if markNtfyErr != nil {
		t.Fatalf("mark ntfy delivered: %v", markNtfyErr)
	}

	markTeamsResult := store.MarkTeamsDelivered(
		ctx,
		teamsDeliveries[0].IntentID(),
		time.Now().UTC(),
		notifications.NewTeamsDeliveredReceipt(200),
	)
	_, markTeamsErr := markTeamsResult.Value()
	if markTeamsErr != nil {
		t.Fatalf("mark microsoft teams delivered: %v", markTeamsErr)
	}

	markZulipResult := store.MarkZulipDelivered(
		ctx,
		zulipDeliveries[0].IntentID(),
		time.Now().UTC(),
		notifications.NewZulipDeliveredReceipt(200),
	)
	_, markZulipErr := markZulipResult.Value()
	if markZulipErr != nil {
		t.Fatalf("mark zulip delivered: %v", markZulipErr)
	}

	assertRepositoryDeliveredIntent(t, ctx, store, "provider-1")
	assertRepositoryWebhookDeliveredIntent(t, ctx, store, 204)
	assertRepositoryEmailDeliveredIntent(t, ctx, store, "<repo-email@example.test>")
	assertRepositoryDiscordDeliveredIntent(t, ctx, store, 204)
	assertRepositoryGoogleChatDeliveredIntent(t, ctx, store, 200)
	assertRepositoryNtfyDeliveredIntent(t, ctx, store, 200)
	assertRepositoryTeamsDeliveredIntent(t, ctx, store, 200)
	assertRepositoryZulipDeliveredIntent(t, ctx, store, 200)
	assertRepositoryEventIDConstraint(t, ctx, store, auth.ProjectID(), event.EventID())
}

func TestPostgresIssueShortIDConcurrency(t *testing.T) {
	ctx := context.Background()
	adminURL := repositoryAdminURL(t)
	databaseURL := createRepositoryTestDatabase(t, ctx, adminURL)
	store, storeErr := NewStore(ctx, databaseURL)
	if storeErr != nil {
		t.Fatalf("store: %v", storeErr)
	}
	defer store.Close()

	migrationResult, migrationErr := store.ApplyMigrations(ctx)
	if migrationErr != nil {
		t.Fatalf("migrate: %v", migrationErr)
	}
	if len(migrationResult.Applied) != 26 {
		t.Fatalf("expected 26 migrations, got %d", len(migrationResult.Applied))
	}

	bootstrap, bootstrapErr := store.Bootstrap(ctx, BootstrapInput{
		PublicURL:        "http://example.test",
		OrganizationName: "Short ID Org",
		ProjectName:      "Short ID API",
		OperatorEmail:    "operator@example.test",
		OperatorPassword: "correct-horse-battery-staple",
	})
	if bootstrapErr != nil {
		t.Fatalf("bootstrap: %v", bootstrapErr)
	}

	ref := mustRepositoryValue(t, domain.NewProjectRef, bootstrap.ProjectRef)
	publicKey := mustRepositoryValue(t, domain.NewProjectPublicKey, bootstrap.PublicKey)
	authResult := store.ResolveProjectKey(ctx, ref, publicKey)
	auth, authErr := authResult.Value()
	if authErr != nil {
		t.Fatalf("resolve project key: %v", authErr)
	}

	ingestConcurrentShortIDEvents(t, ctx, store, auth.OrganizationID(), auth.ProjectID(), 12)
	assertContiguousShortIDs(t, ctx, store, auth.ProjectID(), 12)
	assertShortIDListReferences(t, ctx, store, auth.OrganizationID(), auth.ProjectID(), 12)
}

type repositoryResolver map[string][]netip.Addr

func (resolver repositoryResolver) LookupHost(
	ctx context.Context,
	host string,
) result.Result[[]netip.Addr] {
	addresses, ok := resolver[host]
	if !ok {
		return result.Err[[]netip.Addr](fmt.Errorf("host not found: %s", host))
	}

	return result.Ok(addresses)
}

func claimTelegramConcurrently(
	t *testing.T,
	ctx context.Context,
	store *Store,
) []notifications.TelegramDelivery {
	t.Helper()

	results := make(chan []notifications.TelegramDelivery, 2)
	errors := make(chan error, 2)
	now := time.Now().UTC()
	for range 2 {
		go func() {
			deliveriesResult := store.ClaimTelegramDeliveries(ctx, now, 10)
			deliveries, deliveriesErr := deliveriesResult.Value()
			if deliveriesErr != nil {
				errors <- deliveriesErr
				return
			}

			results <- deliveries
		}()
	}

	return collectTelegramClaims(t, results, errors)
}

func collectTelegramClaims(
	t *testing.T,
	results <-chan []notifications.TelegramDelivery,
	errors <-chan error,
) []notifications.TelegramDelivery {
	t.Helper()

	deliveries := []notifications.TelegramDelivery{}
	for range 2 {
		select {
		case err := <-errors:
			t.Fatalf("claim telegram deliveries: %v", err)
		case claimed := <-results:
			deliveries = append(deliveries, claimed...)
		}
	}

	return deliveries
}

func claimWebhookConcurrently(
	t *testing.T,
	ctx context.Context,
	store *Store,
) []notifications.WebhookDelivery {
	t.Helper()

	results := make(chan []notifications.WebhookDelivery, 2)
	errors := make(chan error, 2)
	now := time.Now().UTC()
	for range 2 {
		go func() {
			deliveriesResult := store.ClaimWebhookDeliveries(ctx, now, 10)
			deliveries, deliveriesErr := deliveriesResult.Value()
			if deliveriesErr != nil {
				errors <- deliveriesErr
				return
			}

			results <- deliveries
		}()
	}

	return collectWebhookClaims(t, results, errors)
}

func collectWebhookClaims(
	t *testing.T,
	results <-chan []notifications.WebhookDelivery,
	errors <-chan error,
) []notifications.WebhookDelivery {
	t.Helper()

	deliveries := []notifications.WebhookDelivery{}
	for range 2 {
		select {
		case err := <-errors:
			t.Fatalf("claim webhook deliveries: %v", err)
		case claimed := <-results:
			deliveries = append(deliveries, claimed...)
		}
	}

	return deliveries
}

func claimEmailConcurrently(
	t *testing.T,
	ctx context.Context,
	store *Store,
) []notifications.EmailDelivery {
	t.Helper()

	results := make(chan []notifications.EmailDelivery, 2)
	errors := make(chan error, 2)
	now := time.Now().UTC()
	for range 2 {
		go func() {
			deliveriesResult := store.ClaimEmailDeliveries(ctx, now, 10)
			deliveries, deliveriesErr := deliveriesResult.Value()
			if deliveriesErr != nil {
				errors <- deliveriesErr
				return
			}

			results <- deliveries
		}()
	}

	return collectEmailClaims(t, results, errors)
}

func collectEmailClaims(
	t *testing.T,
	results <-chan []notifications.EmailDelivery,
	errors <-chan error,
) []notifications.EmailDelivery {
	t.Helper()

	deliveries := []notifications.EmailDelivery{}
	for range 2 {
		select {
		case err := <-errors:
			t.Fatalf("claim email deliveries: %v", err)
		case claimed := <-results:
			deliveries = append(deliveries, claimed...)
		}
	}

	return deliveries
}

func claimDiscordConcurrently(
	t *testing.T,
	ctx context.Context,
	store *Store,
) []notifications.DiscordDelivery {
	t.Helper()

	results := make(chan []notifications.DiscordDelivery, 2)
	errors := make(chan error, 2)
	now := time.Now().UTC()
	for range 2 {
		go func() {
			deliveriesResult := store.ClaimDiscordDeliveries(ctx, now, 10)
			deliveries, deliveriesErr := deliveriesResult.Value()
			if deliveriesErr != nil {
				errors <- deliveriesErr
				return
			}

			results <- deliveries
		}()
	}

	return collectDiscordClaims(t, results, errors)
}

func collectDiscordClaims(
	t *testing.T,
	results <-chan []notifications.DiscordDelivery,
	errors <-chan error,
) []notifications.DiscordDelivery {
	t.Helper()

	deliveries := []notifications.DiscordDelivery{}
	for range 2 {
		select {
		case err := <-errors:
			t.Fatalf("claim discord deliveries: %v", err)
		case claimed := <-results:
			deliveries = append(deliveries, claimed...)
		}
	}

	return deliveries
}

func claimGoogleChatConcurrently(
	t *testing.T,
	ctx context.Context,
	store *Store,
) []notifications.GoogleChatDelivery {
	t.Helper()

	results := make(chan []notifications.GoogleChatDelivery, 2)
	errors := make(chan error, 2)
	now := time.Now().UTC()
	for range 2 {
		go func() {
			deliveriesResult := store.ClaimGoogleChatDeliveries(ctx, now, 10)
			deliveries, deliveriesErr := deliveriesResult.Value()
			if deliveriesErr != nil {
				errors <- deliveriesErr
				return
			}

			results <- deliveries
		}()
	}

	return collectGoogleChatClaims(t, results, errors)
}

func collectGoogleChatClaims(
	t *testing.T,
	results <-chan []notifications.GoogleChatDelivery,
	errors <-chan error,
) []notifications.GoogleChatDelivery {
	t.Helper()

	deliveries := []notifications.GoogleChatDelivery{}
	for range 2 {
		select {
		case err := <-errors:
			t.Fatalf("claim google chat deliveries: %v", err)
		case claimed := <-results:
			deliveries = append(deliveries, claimed...)
		}
	}

	return deliveries
}

func claimNtfyConcurrently(
	t *testing.T,
	ctx context.Context,
	store *Store,
) []notifications.NtfyDelivery {
	t.Helper()

	results := make(chan []notifications.NtfyDelivery, 2)
	errors := make(chan error, 2)
	now := time.Now().UTC()
	for range 2 {
		go func() {
			deliveriesResult := store.ClaimNtfyDeliveries(ctx, now, 10)
			deliveries, deliveriesErr := deliveriesResult.Value()
			if deliveriesErr != nil {
				errors <- deliveriesErr
				return
			}

			results <- deliveries
		}()
	}

	return collectNtfyClaims(t, results, errors)
}

func collectNtfyClaims(
	t *testing.T,
	results <-chan []notifications.NtfyDelivery,
	errors <-chan error,
) []notifications.NtfyDelivery {
	t.Helper()

	deliveries := []notifications.NtfyDelivery{}
	for range 2 {
		select {
		case err := <-errors:
			t.Fatalf("claim ntfy deliveries: %v", err)
		case claimed := <-results:
			deliveries = append(deliveries, claimed...)
		}
	}

	return deliveries
}

func claimTeamsConcurrently(
	t *testing.T,
	ctx context.Context,
	store *Store,
) []notifications.TeamsDelivery {
	t.Helper()

	results := make(chan []notifications.TeamsDelivery, 2)
	errors := make(chan error, 2)
	now := time.Now().UTC()
	for range 2 {
		go func() {
			deliveriesResult := store.ClaimTeamsDeliveries(ctx, now, 10)
			deliveries, deliveriesErr := deliveriesResult.Value()
			if deliveriesErr != nil {
				errors <- deliveriesErr
				return
			}

			results <- deliveries
		}()
	}

	return collectTeamsClaims(t, results, errors)
}

func collectTeamsClaims(
	t *testing.T,
	results <-chan []notifications.TeamsDelivery,
	errors <-chan error,
) []notifications.TeamsDelivery {
	t.Helper()

	deliveries := []notifications.TeamsDelivery{}
	for range 2 {
		select {
		case err := <-errors:
			t.Fatalf("claim microsoft teams deliveries: %v", err)
		case claimed := <-results:
			deliveries = append(deliveries, claimed...)
		}
	}

	return deliveries
}

func claimZulipConcurrently(
	t *testing.T,
	ctx context.Context,
	store *Store,
) []notifications.ZulipDelivery {
	t.Helper()

	results := make(chan []notifications.ZulipDelivery, 2)
	errors := make(chan error, 2)
	now := time.Now().UTC()
	for range 2 {
		go func() {
			deliveriesResult := store.ClaimZulipDeliveries(ctx, now, 10)
			deliveries, deliveriesErr := deliveriesResult.Value()
			if deliveriesErr != nil {
				errors <- deliveriesErr
				return
			}

			results <- deliveries
		}()
	}

	return collectZulipClaims(t, results, errors)
}

func collectZulipClaims(
	t *testing.T,
	results <-chan []notifications.ZulipDelivery,
	errors <-chan error,
) []notifications.ZulipDelivery {
	t.Helper()

	deliveries := []notifications.ZulipDelivery{}
	for range 2 {
		select {
		case err := <-errors:
			t.Fatalf("claim zulip deliveries: %v", err)
		case claimed := <-results:
			deliveries = append(deliveries, claimed...)
		}
	}

	return deliveries
}

func repositoryAdminURL(t *testing.T) string {
	t.Helper()

	value := os.Getenv("ERROR_TRACKER_REPOSITORY_POSTGRES_URL")
	if value != "" {
		return value
	}

	value = os.Getenv("ERROR_TRACKER_E2E_POSTGRES_URL")
	if value != "" {
		return value
	}

	t.Skip("ERROR_TRACKER_REPOSITORY_POSTGRES_URL or ERROR_TRACKER_E2E_POSTGRES_URL is required")
	return ""
}

func createRepositoryTestDatabase(t *testing.T, ctx context.Context, adminURL string) string {
	t.Helper()

	name := fmt.Sprintf("error_tracker_repo_%d", time.Now().UnixNano())
	adminPool, adminErr := pgxpool.New(ctx, adminURL)
	if adminErr != nil {
		t.Fatalf("admin pool: %v", adminErr)
	}
	defer adminPool.Close()

	_, createErr := adminPool.Exec(ctx, "create database "+name)
	if createErr != nil {
		t.Fatalf("create database: %v", createErr)
	}

	t.Cleanup(func() {
		_, _ = adminPool.Exec(context.Background(), "drop database if exists "+name+" with (force)")
	})

	return repositoryDatabaseURL(t, adminURL, name)
}

func repositoryDatabaseURL(t *testing.T, input string, database string) string {
	t.Helper()

	parsed, parseErr := url.Parse(input)
	if parseErr != nil {
		t.Fatalf("database url: %v", parseErr)
	}

	parsed.Path = "/" + database

	return parsed.String()
}

func repositoryEvent(
	t *testing.T,
	organizationID domain.OrganizationID,
	projectID domain.ProjectID,
	eventIDText string,
) domain.CanonicalEvent {
	t.Helper()

	eventID := mustRepositoryValue(t, domain.NewEventID, eventIDText)
	occurredAt := mustRepositoryTimePoint(t, time.Date(2026, 4, 24, 13, 0, 0, 0, time.UTC))
	receivedAt := mustRepositoryTimePoint(t, time.Date(2026, 4, 24, 13, 0, 1, 0, time.UTC))
	title := mustRepositoryTitle(t, "RepositoryError: repository contract failure")
	event, eventErr := domain.NewCanonicalEvent(domain.CanonicalEventParams{
		OrganizationID:       organizationID,
		ProjectID:            projectID,
		EventID:              eventID,
		OccurredAt:           occurredAt,
		ReceivedAt:           receivedAt,
		Kind:                 domain.EventKindError,
		Level:                domain.EventLevelError,
		Title:                title,
		Platform:             "go",
		Release:              "repository@1.0.0",
		Environment:          "test",
		Tags:                 map[string]string{"suite": "repository"},
		DefaultGroupingParts: []string{"RepositoryError", "repository contract failure", "repository_test.go", "TestPostgresRepositoryContract", "100"},
	})
	if eventErr != nil {
		t.Fatalf("canonical event: %v", eventErr)
	}

	return event
}

func shortIDEvent(
	t *testing.T,
	organizationID domain.OrganizationID,
	projectID domain.ProjectID,
	index int,
) domain.CanonicalEvent {
	t.Helper()

	eventID := mustRepositoryValue(
		t,
		domain.NewEventID,
		fmt.Sprintf("770e8400e29b41d4a716%012d", index),
	)
	occurredAt := mustRepositoryTimePoint(t, time.Date(2026, 4, 24, 14, 0, index, 0, time.UTC))
	receivedAt := mustRepositoryTimePoint(t, time.Date(2026, 4, 24, 14, 1, index, 0, time.UTC))
	title := mustRepositoryTitle(t, fmt.Sprintf("ConcurrentError: short id %02d", index))
	event, eventErr := domain.NewCanonicalEvent(domain.CanonicalEventParams{
		OrganizationID:       organizationID,
		ProjectID:            projectID,
		EventID:              eventID,
		OccurredAt:           occurredAt,
		ReceivedAt:           receivedAt,
		Kind:                 domain.EventKindError,
		Level:                domain.EventLevelError,
		Title:                title,
		Platform:             "go",
		Release:              "repository@short-id",
		Environment:          "test",
		Tags:                 map[string]string{"suite": "short-id"},
		DefaultGroupingParts: []string{"ConcurrentError", fmt.Sprintf("short id %02d", index)},
	})
	if eventErr != nil {
		t.Fatalf("canonical event: %v", eventErr)
	}

	return event
}

func ingestConcurrentShortIDEvents(
	t *testing.T,
	ctx context.Context,
	store *Store,
	organizationID domain.OrganizationID,
	projectID domain.ProjectID,
	count int,
) {
	t.Helper()

	errs := make(chan error, count)
	var wg sync.WaitGroup
	for index := 1; index <= count; index++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()

			event := shortIDEvent(t, organizationID, projectID, index)
			receiptResult := ingest.IngestCanonicalEvent(ctx, ingest.NewIngestCommand(event), store)
			receipt, receiptErr := receiptResult.Value()
			if receiptErr != nil {
				errs <- receiptErr
				return
			}

			if receipt.Kind() != ingest.ReceiptAcceptedIssueEvent {
				errs <- fmt.Errorf("unexpected receipt kind: %s", receipt.Kind())
			}
		}(index)
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent ingest: %v", err)
		}
	}
}

func assertContiguousShortIDs(
	t *testing.T,
	ctx context.Context,
	store *Store,
	projectID domain.ProjectID,
	count int,
) {
	t.Helper()

	query := `
select count(*), count(distinct short_id), min(short_id), max(short_id)
from issues
where project_id = $1
`
	var rows int
	var distinctRows int
	var minID int
	var maxID int
	scanErr := store.pool.QueryRow(ctx, query, projectID.String()).Scan(
		&rows,
		&distinctRows,
		&minID,
		&maxID,
	)
	if scanErr != nil {
		t.Fatalf("short id aggregate: %v", scanErr)
	}

	if rows != count || distinctRows != count || minID != 1 || maxID != count {
		t.Fatalf("unexpected short ids rows=%d distinct=%d min=%d max=%d", rows, distinctRows, minID, maxID)
	}
}

func assertShortIDListReferences(
	t *testing.T,
	ctx context.Context,
	store *Store,
	organizationID domain.OrganizationID,
	projectID domain.ProjectID,
	count int,
) {
	t.Helper()

	listResult := issueapp.List(
		ctx,
		store,
		issueapp.IssueListQuery{
			Scope: issueapp.Scope{
				OrganizationID: organizationID,
				ProjectID:      projectID,
			},
			Limit: count,
		},
	)
	list, listErr := listResult.Value()
	if listErr != nil {
		t.Fatalf("issue list: %v", listErr)
	}

	seen := map[int64]bool{}
	for _, item := range list.Items {
		seen[item.ShortID] = true
	}

	for id := int64(1); id <= int64(count); id++ {
		if !seen[id] {
			t.Fatalf("short id %d missing from issue list: %#v", id, list.Items)
		}
	}
}

func assertRepositoryDeliveredIntent(
	t *testing.T,
	ctx context.Context,
	store *Store,
	providerMessageID string,
) {
	t.Helper()

	query := `
select status, provider_message_id, attempts
from notification_intents
where provider_message_id = $1
`
	var status string
	var storedProviderMessageID string
	var attempts int
	scanErr := store.pool.QueryRow(ctx, query, providerMessageID).Scan(
		&status,
		&storedProviderMessageID,
		&attempts,
	)
	if scanErr != nil {
		t.Fatalf("delivered intent: %v", scanErr)
	}

	if status != "delivered" || storedProviderMessageID != providerMessageID || attempts != 1 {
		t.Fatalf("unexpected delivered intent: %s %s %d", status, storedProviderMessageID, attempts)
	}
}

func assertRepositoryWebhookDeliveredIntent(
	t *testing.T,
	ctx context.Context,
	store *Store,
	statusCode int,
) {
	t.Helper()

	query := `
select status, provider_status_code, attempts
from notification_intents
where provider = 'webhook'
`
	var status string
	var storedStatusCode int
	var attempts int
	scanErr := store.pool.QueryRow(ctx, query).Scan(
		&status,
		&storedStatusCode,
		&attempts,
	)
	if scanErr != nil {
		t.Fatalf("webhook delivered intent: %v", scanErr)
	}

	if status != "delivered" || storedStatusCode != statusCode || attempts != 1 {
		t.Fatalf("unexpected webhook intent: %s %d %d", status, storedStatusCode, attempts)
	}
}

func assertRepositoryEmailDeliveredIntent(
	t *testing.T,
	ctx context.Context,
	store *Store,
	providerMessageID string,
) {
	t.Helper()

	query := `
select status, provider_message_id, attempts
from notification_intents
where provider = 'email'
`
	var status string
	var storedProviderMessageID string
	var attempts int
	scanErr := store.pool.QueryRow(ctx, query).Scan(
		&status,
		&storedProviderMessageID,
		&attempts,
	)
	if scanErr != nil {
		t.Fatalf("email delivered intent: %v", scanErr)
	}

	if status != "delivered" || storedProviderMessageID != providerMessageID || attempts != 1 {
		t.Fatalf("unexpected email intent: %s %s %d", status, storedProviderMessageID, attempts)
	}
}

func assertRepositoryDiscordDeliveredIntent(
	t *testing.T,
	ctx context.Context,
	store *Store,
	statusCode int,
) {
	t.Helper()

	query := `
select status, provider_status_code, attempts
from notification_intents
where provider = 'discord'
`
	var status string
	var storedStatusCode int
	var attempts int
	scanErr := store.pool.QueryRow(ctx, query).Scan(
		&status,
		&storedStatusCode,
		&attempts,
	)
	if scanErr != nil {
		t.Fatalf("discord delivered intent: %v", scanErr)
	}

	if status != "delivered" || storedStatusCode != statusCode || attempts != 1 {
		t.Fatalf("unexpected discord intent: %s %d %d", status, storedStatusCode, attempts)
	}
}

func assertRepositoryGoogleChatDeliveredIntent(
	t *testing.T,
	ctx context.Context,
	store *Store,
	statusCode int,
) {
	t.Helper()

	query := `
select status, provider_status_code, attempts
from notification_intents
where provider = 'google_chat'
`
	var status string
	var storedStatusCode int
	var attempts int
	scanErr := store.pool.QueryRow(ctx, query).Scan(
		&status,
		&storedStatusCode,
		&attempts,
	)
	if scanErr != nil {
		t.Fatalf("google chat delivered intent: %v", scanErr)
	}

	if status != "delivered" || storedStatusCode != statusCode || attempts != 1 {
		t.Fatalf("unexpected google chat intent: %s %d %d", status, storedStatusCode, attempts)
	}
}

func assertRepositoryNtfyDeliveredIntent(
	t *testing.T,
	ctx context.Context,
	store *Store,
	statusCode int,
) {
	t.Helper()

	query := `
select status, provider_status_code, attempts
from notification_intents
where provider = 'ntfy'
`
	var status string
	var storedStatusCode int
	var attempts int
	scanErr := store.pool.QueryRow(ctx, query).Scan(
		&status,
		&storedStatusCode,
		&attempts,
	)
	if scanErr != nil {
		t.Fatalf("ntfy delivered intent: %v", scanErr)
	}

	if status != "delivered" || storedStatusCode != statusCode || attempts != 1 {
		t.Fatalf("unexpected ntfy intent: %s %d %d", status, storedStatusCode, attempts)
	}
}

func assertRepositoryTeamsDeliveredIntent(
	t *testing.T,
	ctx context.Context,
	store *Store,
	statusCode int,
) {
	t.Helper()

	query := `
select status, provider_status_code, attempts
from notification_intents
where provider = 'microsoft_teams'
`
	var status string
	var storedStatusCode int
	var attempts int
	scanErr := store.pool.QueryRow(ctx, query).Scan(
		&status,
		&storedStatusCode,
		&attempts,
	)
	if scanErr != nil {
		t.Fatalf("microsoft teams delivered intent: %v", scanErr)
	}

	if status != "delivered" || storedStatusCode != statusCode || attempts != 1 {
		t.Fatalf("unexpected microsoft teams intent: %s %d %d", status, storedStatusCode, attempts)
	}
}

func assertRepositoryZulipDeliveredIntent(
	t *testing.T,
	ctx context.Context,
	store *Store,
	statusCode int,
) {
	t.Helper()

	query := `
select status, provider_status_code, attempts
from notification_intents
where provider = 'zulip'
`
	var status string
	var storedStatusCode int
	var attempts int
	scanErr := store.pool.QueryRow(ctx, query).Scan(
		&status,
		&storedStatusCode,
		&attempts,
	)
	if scanErr != nil {
		t.Fatalf("zulip delivered intent: %v", scanErr)
	}

	if status != "delivered" || storedStatusCode != statusCode || attempts != 1 {
		t.Fatalf("unexpected zulip intent: %s %d %d", status, storedStatusCode, attempts)
	}
}

func assertRepositoryEventIDConstraint(
	t *testing.T,
	ctx context.Context,
	store *Store,
	projectID domain.ProjectID,
	eventID domain.EventID,
) {
	t.Helper()

	query := `select count(*) from events where project_id = $1 and event_id = $2`
	var count int
	scanErr := store.pool.QueryRow(ctx, query, projectID.String(), eventID.String()).Scan(&count)
	if scanErr != nil {
		t.Fatalf("event count: %v", scanErr)
	}

	if count != 1 {
		t.Fatalf("expected one event row after duplicate ingest, got %d", count)
	}
}

func mustRepositoryValue[T any](t *testing.T, constructor func(string) (T, error), input string) T {
	t.Helper()

	value, err := constructor(input)
	if err != nil {
		t.Fatalf("domain value: %v", err)
	}

	return value
}

func auditActionsByName(view auditapp.View) map[string]bool {
	actions := map[string]bool{}
	for _, event := range view.Events {
		actions[event.Action] = true
	}

	return actions
}

func assignmentOptionByKind(
	t *testing.T,
	detail issueapp.IssueDetailView,
	kind string,
) issueapp.AssigneeOptionView {
	t.Helper()

	for _, option := range detail.Assignees {
		if option.Kind == kind {
			return option
		}
	}

	t.Fatalf("assignment option kind %s not found in %#v", kind, detail.Assignees)
	return issueapp.AssigneeOptionView{}
}

func mustRepositoryTitle(t *testing.T, input string) domain.EventTitle {
	t.Helper()

	title, titleErr := domain.NewEventTitle(input)
	if titleErr != nil {
		t.Fatalf("title: %v", titleErr)
	}

	return title
}

func mustRepositoryTimePoint(t *testing.T, value time.Time) domain.TimePoint {
	t.Helper()

	point, pointErr := domain.NewTimePoint(value)
	if pointErr != nil {
		t.Fatalf("time point: %v", pointErr)
	}

	return point
}
