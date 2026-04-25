-- Canonical copy lives in internal/adapters/postgres/migrations for one-binary embedding.
-- This root file is kept as an operator-visible migration carrier.

create table if not exists project_retention_policies (
  organization_id uuid not null references organizations(id),
  project_id uuid not null references projects(id),
  event_retention_days integer not null default 90 check (event_retention_days > 0),
  payload_retention_days integer not null default 30 check (payload_retention_days > 0),
  delivery_retention_days integer not null default 30 check (delivery_retention_days > 0),
  user_report_retention_days integer not null default 90 check (user_report_retention_days > 0),
  enabled boolean not null default true,
  created_at timestamptz not null,
  updated_at timestamptz not null,
  primary key (project_id),
  unique (organization_id, project_id)
);

insert into project_retention_policies (
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

create index if not exists project_retention_policies_enabled_idx
  on project_retention_policies(organization_id, project_id)
  where enabled = true;

alter table user_reports
  drop constraint if exists user_reports_issue_id_fkey;

alter table user_reports
  add constraint user_reports_issue_id_fkey
  foreign key (issue_id) references issues(id) on delete set null;
