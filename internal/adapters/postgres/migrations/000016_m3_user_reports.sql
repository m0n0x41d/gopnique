-- Canonical copy lives in internal/adapters/postgres/migrations for one-binary embedding.
-- This root file is kept as an operator-visible migration carrier.

create table if not exists user_reports (
  id uuid primary key,
  organization_id uuid not null references organizations(id),
  project_id uuid not null references projects(id),
  issue_id uuid references issues(id) on delete cascade,
  event_id uuid not null,
  name text not null,
  email text not null,
  comments text not null,
  created_at timestamptz not null,
  unique (project_id, event_id)
);

create index if not exists user_reports_project_created_idx
  on user_reports(project_id, created_at desc);

create index if not exists user_reports_issue_created_idx
  on user_reports(issue_id, created_at desc)
  where issue_id is not null;
