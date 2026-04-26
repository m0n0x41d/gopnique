alter table audit_events
  drop constraint if exists audit_events_action_check;

alter table audit_events
  add constraint audit_events_action_check
  check (action in (
    'bootstrap',
    'api_token_created',
    'api_token_revoked',
    'issue_assigned',
    'issue_comment_created',
    'issue_status_changed',
    'monitor_created',
    'monitor_state_changed',
    'status_page_created',
    'import_run_completed'
  ));

create table if not exists import_runs (
  id uuid primary key,
  organization_id uuid not null references organizations(id),
  project_id uuid not null references projects(id),
  source_system text not null check (length(btrim(source_system)) between 1 and 64),
  mode text not null check (mode = 'apply'),
  status text not null check (status in ('running', 'completed', 'completed_with_errors', 'failed')),
  total_rows integer not null default 0 check (total_rows >= 0),
  applied_rows integer not null default 0 check (applied_rows >= 0),
  duplicate_rows integer not null default 0 check (duplicate_rows >= 0),
  skipped_rows integer not null default 0 check (skipped_rows >= 0),
  failed_rows integer not null default 0 check (failed_rows >= 0),
  started_at timestamptz not null,
  finished_at timestamptz
);

create index if not exists import_runs_project_started_idx
  on import_runs(project_id, started_at desc);

create table if not exists import_records (
  id uuid primary key,
  import_run_id uuid not null references import_runs(id),
  organization_id uuid not null references organizations(id),
  project_id uuid not null references projects(id),
  source_system text not null check (length(btrim(source_system)) between 1 and 64),
  external_id text not null check (length(btrim(external_id)) between 1 and 256),
  record_kind text not null check (record_kind in ('issue', 'event')),
  status text not null check (status in ('pending', 'applied', 'duplicate', 'failed')),
  event_id uuid,
  issue_id uuid references issues(id),
  error text,
  row_number integer not null check (row_number > 0),
  created_at timestamptz not null,
  updated_at timestamptz not null,
  unique (project_id, source_system, external_id)
);

create index if not exists import_records_run_row_idx
  on import_records(import_run_id, row_number);

create index if not exists import_records_project_status_idx
  on import_records(project_id, status, updated_at desc);
