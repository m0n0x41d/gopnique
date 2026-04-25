-- Canonical copy lives in internal/adapters/postgres/migrations for one-binary embedding.
-- This root file is kept as an operator-visible migration carrier.

create table if not exists organization_quota_policies (
  organization_id uuid primary key references organizations(id),
  daily_event_limit integer not null default 100000 check (daily_event_limit > 0),
  enabled boolean not null default false,
  created_at timestamptz not null,
  updated_at timestamptz not null
);

create table if not exists project_quota_policies (
  organization_id uuid not null references organizations(id),
  project_id uuid primary key references projects(id),
  daily_event_limit integer not null default 100000 check (daily_event_limit > 0),
  enabled boolean not null default false,
  created_at timestamptz not null,
  updated_at timestamptz not null,
  unique (organization_id, project_id)
);

insert into organization_quota_policies (
  organization_id,
  created_at,
  updated_at
)
select
  o.id,
  now(),
  now()
from organizations o
on conflict (organization_id) do nothing;

insert into project_quota_policies (
  organization_id,
  project_id,
  created_at,
  updated_at
)
select
  p.organization_id,
  p.id,
  now(),
  now()
from projects p
on conflict (project_id) do nothing;

create index if not exists project_hourly_stats_org_bucket_idx
  on project_hourly_stats(organization_id, bucket_at);
