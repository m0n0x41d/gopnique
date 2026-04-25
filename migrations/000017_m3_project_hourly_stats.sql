-- Canonical copy lives in internal/adapters/postgres/migrations for one-binary embedding.
-- This root file is kept as an operator-visible migration carrier.

create table if not exists project_hourly_stats (
  organization_id uuid not null references organizations(id),
  project_id uuid not null references projects(id),
  bucket_at timestamptz not null,
  event_count bigint not null default 0 check (event_count >= 0),
  issue_event_count bigint not null default 0 check (issue_event_count >= 0),
  transaction_event_count bigint not null default 0 check (transaction_event_count >= 0),
  primary key (project_id, bucket_at)
);

create index if not exists project_hourly_stats_org_project_bucket_idx
  on project_hourly_stats(organization_id, project_id, bucket_at desc);
