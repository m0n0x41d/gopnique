-- Canonical copy lives in internal/adapters/postgres/migrations for one-binary embedding.
-- This root file is kept as an operator-visible migration carrier.

create table if not exists project_key_rate_limit_policies (
  project_key_id uuid primary key references project_keys(id) on delete cascade,
  organization_id uuid not null references organizations(id),
  project_id uuid not null references projects(id),
  window_seconds integer not null default 60 check (window_seconds > 0),
  event_limit integer not null default 600 check (event_limit > 0),
  enabled boolean not null default false,
  created_at timestamptz not null,
  updated_at timestamptz not null,
  unique (organization_id, project_id, project_key_id)
);

create table if not exists project_key_rate_limit_buckets (
  project_key_id uuid not null references project_keys(id) on delete cascade,
  organization_id uuid not null references organizations(id),
  project_id uuid not null references projects(id),
  bucket_at timestamptz not null,
  window_seconds integer not null check (window_seconds > 0),
  event_count integer not null default 0 check (event_count >= 0),
  created_at timestamptz not null,
  updated_at timestamptz not null,
  primary key (project_key_id, bucket_at, window_seconds)
);

insert into project_key_rate_limit_policies (
  project_key_id,
  organization_id,
  project_id,
  created_at,
  updated_at
)
select
  pk.id,
  p.organization_id,
  pk.project_id,
  now(),
  now()
from project_keys pk
join projects p on p.id = pk.project_id
on conflict (project_key_id) do nothing;

create index if not exists project_key_rate_limit_buckets_project_bucket_idx
  on project_key_rate_limit_buckets(project_id, bucket_at desc);
