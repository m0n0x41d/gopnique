create table if not exists teams (
  id uuid primary key,
  organization_id uuid not null references organizations(id),
  slug text not null,
  name text not null,
  created_at timestamptz not null,
  unique (organization_id, slug)
);

create table if not exists team_memberships (
  team_id uuid not null references teams(id),
  organization_id uuid not null references organizations(id),
  operator_id uuid not null references operators(id),
  role text not null check (role in ('manager', 'member')),
  created_at timestamptz not null,
  primary key (team_id, operator_id)
);

create table if not exists team_project_memberships (
  team_id uuid not null references teams(id),
  organization_id uuid not null references organizations(id),
  project_id uuid not null references projects(id),
  role text not null check (role in ('admin', 'member')),
  created_at timestamptz not null,
  primary key (team_id, project_id)
);

create index if not exists team_memberships_operator_idx
  on team_memberships(operator_id, team_id);

create index if not exists team_project_memberships_project_idx
  on team_project_memberships(project_id, team_id);

with default_teams as (
  select
    (
      substr(md5(o.id::text || ':default-team'), 1, 8) || '-' ||
      substr(md5(o.id::text || ':default-team'), 9, 4) || '-' ||
      substr(md5(o.id::text || ':default-team'), 13, 4) || '-' ||
      substr(md5(o.id::text || ':default-team'), 17, 4) || '-' ||
      substr(md5(o.id::text || ':default-team'), 21, 12)
    )::uuid as id,
    o.id as organization_id
  from organizations o
)
insert into teams (id, organization_id, slug, name, created_at)
select id, organization_id, 'default', 'Default team', now()
from default_teams
on conflict (organization_id, slug) do nothing;

insert into team_memberships (
  team_id,
  organization_id,
  operator_id,
  role,
  created_at
)
select
  t.id,
  oo.organization_id,
  oo.operator_id,
  'manager',
  now()
from operator_organizations oo
join teams t on t.organization_id = oo.organization_id and t.slug = 'default'
on conflict (team_id, operator_id) do nothing;

insert into team_project_memberships (
  team_id,
  organization_id,
  project_id,
  role,
  created_at
)
select
  t.id,
  p.organization_id,
  p.id,
  'admin',
  now()
from projects p
join teams t on t.organization_id = p.organization_id and t.slug = 'default'
on conflict (team_id, project_id) do nothing;
