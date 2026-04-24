package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"

	memberapp "github.com/ivanzakutnii/error-tracker/internal/app/members"
	"github.com/ivanzakutnii/error-tracker/internal/kernel/result"
)

func (store *Store) ShowMembers(
	ctx context.Context,
	query memberapp.Query,
) result.Result[memberapp.View] {
	operatorsResult := store.listOperators(ctx, query.Scope)
	operators, operatorsErr := operatorsResult.Value()
	if operatorsErr != nil {
		return result.Err[memberapp.View](operatorsErr)
	}

	teamsResult := store.listTeams(ctx, query.Scope)
	teams, teamsErr := teamsResult.Value()
	if teamsErr != nil {
		return result.Err[memberapp.View](teamsErr)
	}

	return result.Ok(memberapp.View{
		Operators: operators,
		Teams:     teams,
	})
}

func (store *Store) listOperators(
	ctx context.Context,
	scope memberapp.Scope,
) result.Result[[]memberapp.OperatorView] {
	query := `
select
  o.id,
  o.email,
  o.display_name,
  oo.role,
  pm.role,
  o.active
from operators o
join operator_organizations oo on oo.operator_id = o.id
join project_memberships pm on pm.operator_id = o.id and pm.organization_id = oo.organization_id
where oo.organization_id = $1
  and pm.project_id = $2
order by o.email asc
`
	rows, queryErr := store.pool.Query(
		ctx,
		query,
		scope.OrganizationID.String(),
		scope.ProjectID.String(),
	)
	if queryErr != nil {
		return result.Err[[]memberapp.OperatorView](queryErr)
	}
	defer rows.Close()

	operators := []memberapp.OperatorView{}
	for rows.Next() {
		operator, operatorErr := scanOperatorView(rows)
		if operatorErr != nil {
			return result.Err[[]memberapp.OperatorView](operatorErr)
		}

		operators = append(operators, operator)
	}

	if rowsErr := rows.Err(); rowsErr != nil {
		return result.Err[[]memberapp.OperatorView](rowsErr)
	}

	return result.Ok(operators)
}

func (store *Store) listTeams(
	ctx context.Context,
	scope memberapp.Scope,
) result.Result[[]memberapp.TeamView] {
	query := `
select
  t.id,
  t.name,
  t.slug,
  count(tm.operator_id),
  coalesce(max(tm.role), ''),
  tpm.role
from teams t
join team_project_memberships tpm on tpm.team_id = t.id
left join team_memberships tm on tm.team_id = t.id
where t.organization_id = $1
  and tpm.project_id = $2
group by t.id, t.name, t.slug, tpm.role
order by t.name asc
`
	rows, queryErr := store.pool.Query(
		ctx,
		query,
		scope.OrganizationID.String(),
		scope.ProjectID.String(),
	)
	if queryErr != nil {
		return result.Err[[]memberapp.TeamView](queryErr)
	}
	defer rows.Close()

	teams := []memberapp.TeamView{}
	for rows.Next() {
		team, teamErr := scanTeamView(rows)
		if teamErr != nil {
			return result.Err[[]memberapp.TeamView](teamErr)
		}

		teams = append(teams, team)
	}

	if rowsErr := rows.Err(); rowsErr != nil {
		return result.Err[[]memberapp.TeamView](rowsErr)
	}

	return result.Ok(teams)
}

func scanOperatorView(rows pgx.Rows) (memberapp.OperatorView, error) {
	var operator memberapp.OperatorView
	var active bool
	scanErr := rows.Scan(
		&operator.ID,
		&operator.Email,
		&operator.DisplayName,
		&operator.OrgRole,
		&operator.ProjectRole,
		&active,
	)
	if scanErr != nil {
		return memberapp.OperatorView{}, scanErr
	}

	operator.Status = statusFromActive(active)

	return operator, nil
}

func scanTeamView(rows pgx.Rows) (memberapp.TeamView, error) {
	var team memberapp.TeamView
	scanErr := rows.Scan(
		&team.ID,
		&team.Name,
		&team.Slug,
		&team.MemberCount,
		&team.MemberRole,
		&team.ProjectRole,
	)
	if scanErr != nil {
		return memberapp.TeamView{}, scanErr
	}

	return team, nil
}

func statusFromActive(active bool) string {
	if active {
		return "active"
	}

	return "disabled"
}
