package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	issueapp "github.com/ivanzakutnii/error-tracker/internal/app/issues"
	"github.com/ivanzakutnii/error-tracker/internal/kernel/result"
)

func (store *Store) ListIssues(
	ctx context.Context,
	query issueapp.IssueListQuery,
) result.Result[issueapp.IssueListView] {
	statement := buildIssueListStatement(query)

	rows, rowsErr := store.pool.Query(ctx, statement.sql, statement.args...)
	if rowsErr != nil {
		return result.Err[issueapp.IssueListView](rowsErr)
	}
	defer rows.Close()

	items := []issueapp.IssueSummaryView{}

	for rows.Next() {
		item, scanErr := scanIssueSummary(rows)
		if scanErr != nil {
			return result.Err[issueapp.IssueListView](scanErr)
		}

		items = append(items, item)
	}

	if rows.Err() != nil {
		return result.Err[issueapp.IssueListView](rows.Err())
	}

	return result.Ok(issueapp.IssueListView{
		Status: string(query.Status),
		Search: query.Search.Canonical(),
		Items:  items,
	})
}

type issueListStatement struct {
	sql  string
	args []any
}

func buildIssueListStatement(query issueapp.IssueListQuery) issueListStatement {
	args := []any{
		query.Scope.OrganizationID.String(),
		query.Scope.ProjectID.String(),
	}
	clauses := []string{
		"i.organization_id = $1",
		"i.project_id = $2",
	}
	addArg := func(value any) string {
		args = append(args, value)
		return fmt.Sprintf("$%d", len(args))
	}

	clauses = append(clauses, "i.status = "+addArg(string(query.Status)))
	if query.Search.Environment != "" {
		clauses = append(clauses, "i.environment = "+addArg(query.Search.Environment))
	}
	if query.Search.Release != "" {
		clauses = append(clauses, "i.release = "+addArg(query.Search.Release))
	}
	if query.Search.Level != "" {
		clauses = append(clauses, "lower(coalesce(e.level, '')) = lower("+addArg(query.Search.Level)+")")
	}
	if query.Search.Text != "" {
		clauses = append(clauses, "i.title ilike "+addArg("%"+query.Search.Text+"%"))
	}
	if query.Search.TagKey != "" {
		tagKeyArg := addArg(query.Search.TagKey)
		tagValueArg := addArg(query.Search.TagValue)
		clauses = append(clauses, "exists (select 1 from event_tags et where et.event_id = i.last_event_id and et.key = "+tagKeyArg+" and et.value = "+tagValueArg+")")
	}
	if query.Search.Assignee.Valid() {
		clauses = append(clauses, assigneeClause(query.Search.Assignee, addArg))
	}
	if query.Search.LastSeenAfter != nil {
		clauses = append(clauses, "i.last_seen_at >= "+addArg(*query.Search.LastSeenAfter))
	}
	if query.Search.LastSeenBefore != nil {
		clauses = append(clauses, "i.last_seen_at <= "+addArg(*query.Search.LastSeenBefore))
	}

	limitArg := addArg(query.Limit)
	sql := `
select
  i.id,
  i.short_id,
  i.title,
  i.type,
  i.status,
  i.event_count,
  i.last_event_id,
  coalesce(e.level, ''),
  coalesce(e.platform, ''),
  i.last_seen_at,
  coalesce(i.environment, ''),
  coalesce(i.release, ''),
  coalesce(assignee_team.name, assignee_operator.email, '')
from issues i
join events e on e.id = i.last_event_id
left join operators assignee_operator on assignee_operator.id = i.assignee_operator_id
left join teams assignee_team on assignee_team.id = i.assignee_team_id
where ` + strings.Join(clauses, "\n  and ") + `
order by i.last_seen_at desc
limit ` + limitArg + `
`

	return issueListStatement{sql: sql, args: args}
}

func assigneeClause(
	target issueapp.AssignmentTarget,
	addArg func(any) string,
) string {
	if target.Kind == issueapp.AssignmentTargetNone {
		return "i.assignee_operator_id is null and i.assignee_team_id is null"
	}

	if target.Kind == issueapp.AssignmentTargetOperator {
		return "i.assignee_operator_id = " + addArg(target.ID)
	}

	return "i.assignee_team_id = " + addArg(target.ID)
}

func (store *Store) ShowIssue(
	ctx context.Context,
	query issueapp.IssueDetailQuery,
) result.Result[issueapp.IssueDetailView] {
	sql := `
select
  i.id,
  i.short_id,
  i.title,
  i.type,
  i.status,
  i.event_count,
  i.first_seen_at,
  i.last_seen_at,
  i.last_event_id,
  coalesce(e.level, ''),
  coalesce(e.platform, ''),
  e.fingerprint,
  coalesce(i.environment, ''),
  coalesce(i.release, ''),
  coalesce(assignee_team.name, assignee_operator.email, '')
from issues i
join events e on e.id = i.last_event_id
left join operators assignee_operator on assignee_operator.id = i.assignee_operator_id
left join teams assignee_team on assignee_team.id = i.assignee_team_id
where i.id = $1
  and i.organization_id = $2
  and i.project_id = $3
`

	var view issueapp.IssueDetailView
	var firstSeen time.Time
	var lastSeen time.Time
	scanErr := store.pool.QueryRow(
		ctx,
		sql,
		query.IssueID.String(),
		query.Scope.OrganizationID.String(),
		query.Scope.ProjectID.String(),
	).Scan(
		&view.ID,
		&view.ShortID,
		&view.Title,
		&view.Type,
		&view.Status,
		&view.EventCount,
		&firstSeen,
		&lastSeen,
		&view.LatestEventID,
		&view.LatestLevel,
		&view.LatestPlatform,
		&view.Fingerprint,
		&view.Environment,
		&view.Release,
		&view.Assignee,
	)
	if scanErr != nil {
		return result.Err[issueapp.IssueDetailView](scanErr)
	}

	view.FirstSeen = formatTime(firstSeen)
	view.LastSeen = formatTime(lastSeen)
	tagsResult := store.eventTags(ctx, view.LatestEventID)
	tags, tagsErr := tagsResult.Value()
	if tagsErr != nil {
		return result.Err[issueapp.IssueDetailView](tagsErr)
	}
	view.Tags = tags
	commentsResult := store.issueComments(ctx, view.ID)
	comments, commentsErr := commentsResult.Value()
	if commentsErr != nil {
		return result.Err[issueapp.IssueDetailView](commentsErr)
	}
	view.Comments = comments
	assigneesResult := store.assignmentOptions(ctx, query.Scope)
	assignees, assigneesErr := assigneesResult.Value()
	if assigneesErr != nil {
		return result.Err[issueapp.IssueDetailView](assigneesErr)
	}
	view.Assignees = assignees

	return result.Ok(view)
}

func (store *Store) ShowEvent(
	ctx context.Context,
	query issueapp.EventDetailQuery,
) result.Result[issueapp.EventDetailView] {
	sql := `
select
  e.id,
  e.event_id,
  coalesce(i.id::text, ''),
  e.title,
  e.kind,
  coalesce(e.level, ''),
  e.platform,
  e.occurred_at,
  e.received_at,
  e.fingerprint,
  coalesce(e.environment, ''),
  coalesce(e.release, ''),
  coalesce(e.canonical_payload::text, '{}')
from events e
left join issue_fingerprints f on f.project_id = e.project_id and f.fingerprint = e.fingerprint
left join issues i on i.id = f.issue_id
where (e.event_id = $1 or e.id = $1)
  and e.organization_id = $2
  and e.project_id = $3
`

	var view issueapp.EventDetailView
	var occurredAt time.Time
	var receivedAt time.Time
	scanErr := store.pool.QueryRow(
		ctx,
		sql,
		query.EventID.String(),
		query.Scope.OrganizationID.String(),
		query.Scope.ProjectID.String(),
	).Scan(
		&view.ID,
		&view.EventID,
		&view.IssueID,
		&view.Title,
		&view.Kind,
		&view.Level,
		&view.Platform,
		&occurredAt,
		&receivedAt,
		&view.Fingerprint,
		&view.Environment,
		&view.Release,
		&view.PayloadJSON,
	)
	if scanErr != nil {
		return result.Err[issueapp.EventDetailView](scanErr)
	}

	view.OccurredAt = formatTime(occurredAt)
	view.ReceivedAt = formatTime(receivedAt)
	tagsResult := store.eventTags(ctx, view.ID)
	tags, tagsErr := tagsResult.Value()
	if tagsErr != nil {
		return result.Err[issueapp.EventDetailView](tagsErr)
	}
	view.Tags = tags

	return result.Ok(view)
}

func scanIssueSummary(rows pgx.Rows) (issueapp.IssueSummaryView, error) {
	var item issueapp.IssueSummaryView
	var lastSeen time.Time

	scanErr := rows.Scan(
		&item.ID,
		&item.ShortID,
		&item.Title,
		&item.Type,
		&item.Status,
		&item.EventCount,
		&item.LatestEventID,
		&item.Level,
		&item.Platform,
		&lastSeen,
		&item.Environment,
		&item.Release,
		&item.Assignee,
	)
	if scanErr != nil {
		return issueapp.IssueSummaryView{}, scanErr
	}

	item.LastSeen = formatTime(lastSeen)

	return item, nil
}

func (store *Store) eventTags(
	ctx context.Context,
	eventRowID string,
) result.Result[[]issueapp.TagView] {
	query := `
select key, value
from event_tags
where event_id = $1
order by key asc
`
	rows, rowsErr := store.pool.Query(ctx, query, eventRowID)
	if rowsErr != nil {
		return result.Err[[]issueapp.TagView](rowsErr)
	}
	defer rows.Close()

	tags := []issueapp.TagView{}
	for rows.Next() {
		var tag issueapp.TagView
		scanErr := rows.Scan(&tag.Key, &tag.Value)
		if scanErr != nil {
			return result.Err[[]issueapp.TagView](scanErr)
		}

		tags = append(tags, tag)
	}

	if rows.Err() != nil {
		return result.Err[[]issueapp.TagView](rows.Err())
	}

	return result.Ok(tags)
}

func (store *Store) issueComments(
	ctx context.Context,
	issueID string,
) result.Result[[]issueapp.CommentView] {
	query := `
select
  c.id,
  coalesce(o.email, 'system'),
  c.body,
  c.created_at
from issue_comments c
left join operators o on o.id = c.actor_operator_id
where c.issue_id = $1
order by c.created_at asc
`
	rows, rowsErr := store.pool.Query(ctx, query, issueID)
	if rowsErr != nil {
		return result.Err[[]issueapp.CommentView](rowsErr)
	}
	defer rows.Close()

	comments := []issueapp.CommentView{}
	for rows.Next() {
		var comment issueapp.CommentView
		var createdAt time.Time
		scanErr := rows.Scan(
			&comment.ID,
			&comment.Actor,
			&comment.Body,
			&createdAt,
		)
		if scanErr != nil {
			return result.Err[[]issueapp.CommentView](scanErr)
		}

		comment.CreatedAt = formatTime(createdAt)
		comments = append(comments, comment)
	}

	if rows.Err() != nil {
		return result.Err[[]issueapp.CommentView](rows.Err())
	}

	return result.Ok(comments)
}

func (store *Store) assignmentOptions(
	ctx context.Context,
	scope issueapp.Scope,
) result.Result[[]issueapp.AssigneeOptionView] {
	operatorOptionsResult := store.operatorAssignmentOptions(ctx, scope)
	operatorOptions, operatorOptionsErr := operatorOptionsResult.Value()
	if operatorOptionsErr != nil {
		return result.Err[[]issueapp.AssigneeOptionView](operatorOptionsErr)
	}

	teamOptionsResult := store.teamAssignmentOptions(ctx, scope)
	teamOptions, teamOptionsErr := teamOptionsResult.Value()
	if teamOptionsErr != nil {
		return result.Err[[]issueapp.AssigneeOptionView](teamOptionsErr)
	}

	options := []issueapp.AssigneeOptionView{{
		Value: "none",
		Label: "Unassigned",
		Kind:  "none",
	}}
	options = append(options, operatorOptions...)
	options = append(options, teamOptions...)

	return result.Ok(options)
}

func (store *Store) operatorAssignmentOptions(
	ctx context.Context,
	scope issueapp.Scope,
) result.Result[[]issueapp.AssigneeOptionView] {
	query := `
select o.id, o.email
from project_memberships pm
join operators o on o.id = pm.operator_id and o.active = true
where pm.organization_id = $1
  and pm.project_id = $2
order by o.email asc
`
	rows, rowsErr := store.pool.Query(ctx, query, scope.OrganizationID.String(), scope.ProjectID.String())
	if rowsErr != nil {
		return result.Err[[]issueapp.AssigneeOptionView](rowsErr)
	}
	defer rows.Close()

	options := []issueapp.AssigneeOptionView{}
	for rows.Next() {
		var id string
		var label string
		scanErr := rows.Scan(&id, &label)
		if scanErr != nil {
			return result.Err[[]issueapp.AssigneeOptionView](scanErr)
		}

		options = append(options, issueapp.AssigneeOptionView{
			Value: "operator:" + id,
			Label: label,
			Kind:  "operator",
		})
	}

	if rows.Err() != nil {
		return result.Err[[]issueapp.AssigneeOptionView](rows.Err())
	}

	return result.Ok(options)
}

func (store *Store) teamAssignmentOptions(
	ctx context.Context,
	scope issueapp.Scope,
) result.Result[[]issueapp.AssigneeOptionView] {
	query := `
select t.id, t.name
from team_project_memberships tpm
join teams t on t.id = tpm.team_id
where tpm.organization_id = $1
  and tpm.project_id = $2
order by t.name asc
`
	rows, rowsErr := store.pool.Query(ctx, query, scope.OrganizationID.String(), scope.ProjectID.String())
	if rowsErr != nil {
		return result.Err[[]issueapp.AssigneeOptionView](rowsErr)
	}
	defer rows.Close()

	options := []issueapp.AssigneeOptionView{}
	for rows.Next() {
		var id string
		var label string
		scanErr := rows.Scan(&id, &label)
		if scanErr != nil {
			return result.Err[[]issueapp.AssigneeOptionView](scanErr)
		}

		options = append(options, issueapp.AssigneeOptionView{
			Value: "team:" + id,
			Label: label,
			Kind:  "team",
		})
	}

	if rows.Err() != nil {
		return result.Err[[]issueapp.AssigneeOptionView](rows.Err())
	}

	return result.Ok(options)
}

func formatTime(value time.Time) string {
	return value.UTC().Format("2006-01-02 15:04:05 UTC")
}
