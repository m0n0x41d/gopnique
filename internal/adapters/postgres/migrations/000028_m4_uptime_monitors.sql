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
    'monitor_state_changed'
  ));

create table if not exists uptime_monitors (
  id uuid primary key,
  organization_id uuid not null references organizations(id),
  project_id uuid not null references projects(id),
  monitor_type text not null check (monitor_type = 'http'),
  name text not null check (length(btrim(name)) between 1 and 128),
  target_url text not null,
  interval_seconds integer not null check (interval_seconds between 60 and 86400),
  timeout_seconds integer not null check (timeout_seconds between 1 and 30),
  enabled boolean not null default true,
  current_state text not null default 'unknown' check (current_state in ('unknown', 'up', 'down')),
  last_checked_at timestamptz,
  next_check_at timestamptz not null,
  check_lease_until timestamptz,
  created_by_operator_id uuid references operators(id),
  created_at timestamptz not null,
  updated_at timestamptz not null,
  unique (project_id, name)
);

create index if not exists uptime_monitors_project_created_idx
  on uptime_monitors(project_id, created_at desc);

create index if not exists uptime_monitors_due_idx
  on uptime_monitors(next_check_at asc)
  where enabled = true;

create table if not exists uptime_monitor_checks (
  id uuid primary key,
  organization_id uuid not null references organizations(id),
  project_id uuid not null references projects(id),
  monitor_id uuid not null references uptime_monitors(id) on delete cascade,
  status text not null check (status in ('up', 'down')),
  http_status integer check (http_status between 100 and 599),
  duration_ms double precision not null check (duration_ms >= 0),
  error text not null default '',
  checked_at timestamptz not null
);

create index if not exists uptime_monitor_checks_monitor_checked_idx
  on uptime_monitor_checks(monitor_id, checked_at desc);

create table if not exists uptime_monitor_incidents (
  id uuid primary key,
  organization_id uuid not null references organizations(id),
  project_id uuid not null references projects(id),
  monitor_id uuid not null references uptime_monitors(id) on delete cascade,
  opened_at timestamptz not null,
  resolved_at timestamptz,
  last_check_id uuid references uptime_monitor_checks(id),
  reason text not null default ''
);

create unique index if not exists uptime_monitor_incidents_one_open_idx
  on uptime_monitor_incidents(monitor_id)
  where resolved_at is null;

create index if not exists uptime_monitor_incidents_project_opened_idx
  on uptime_monitor_incidents(project_id, opened_at desc);
