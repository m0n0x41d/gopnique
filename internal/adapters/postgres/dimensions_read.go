package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	dimensionapp "github.com/ivanzakutnii/error-tracker/internal/app/dimensions"
	"github.com/ivanzakutnii/error-tracker/internal/kernel/result"
)

func (store *Store) ListEnvironments(
	ctx context.Context,
	query dimensionapp.Query,
) result.Result[dimensionapp.View] {
	return store.listDimension(ctx, query, dimensionapp.KindEnvironment, "i.environment")
}

func (store *Store) ListReleases(
	ctx context.Context,
	query dimensionapp.Query,
) result.Result[dimensionapp.View] {
	return store.listDimension(ctx, query, dimensionapp.KindRelease, "i.release")
}

func (store *Store) listDimension(
	ctx context.Context,
	query dimensionapp.Query,
	kind dimensionapp.Kind,
	column string,
) result.Result[dimensionapp.View] {
	sql := `
with scoped_issues as (
  select
    nullif(btrim(` + column + `), '') as value,
    i.status,
    i.last_seen_at
  from issues i
  where i.organization_id = $1
    and i.project_id = $2
)
select
  value,
  count(*),
  count(*) filter (where status = 'unresolved'),
  count(*) filter (where status = 'resolved'),
  count(*) filter (where status = 'ignored'),
  max(last_seen_at)
from scoped_issues
where value is not null
group by value
order by max(last_seen_at) desc, value asc
limit $3
`
	rows, queryErr := store.pool.Query(
		ctx,
		sql,
		query.Scope.OrganizationID.String(),
		query.Scope.ProjectID.String(),
		query.Limit,
	)
	if queryErr != nil {
		return result.Err[dimensionapp.View](queryErr)
	}
	defer rows.Close()

	itemsResult := scanDimensionItems(rows)
	items, itemsErr := itemsResult.Value()
	if itemsErr != nil {
		return result.Err[dimensionapp.View](itemsErr)
	}

	return result.Ok(dimensionapp.View{
		Kind:  kind,
		Items: items,
	})
}

func scanDimensionItems(rows pgx.Rows) result.Result[[]dimensionapp.ItemView] {
	items := []dimensionapp.ItemView{}

	for rows.Next() {
		item, scanErr := scanDimensionItem(rows)
		if scanErr != nil {
			return result.Err[[]dimensionapp.ItemView](scanErr)
		}

		items = append(items, item)
	}

	if rows.Err() != nil {
		return result.Err[[]dimensionapp.ItemView](rows.Err())
	}

	return result.Ok(items)
}

func scanDimensionItem(rows pgx.Rows) (dimensionapp.ItemView, error) {
	var item dimensionapp.ItemView
	var lastSeen time.Time
	scanErr := rows.Scan(
		&item.Name,
		&item.IssueCount,
		&item.UnresolvedCount,
		&item.ResolvedCount,
		&item.IgnoredCount,
		&lastSeen,
	)
	if scanErr != nil {
		return dimensionapp.ItemView{}, scanErr
	}

	item.LastSeen = formatTime(lastSeen)

	return item, nil
}
