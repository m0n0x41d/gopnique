create table if not exists log_records (
  id uuid primary key,
  organization_id uuid not null references organizations(id),
  project_id uuid not null references projects(id),
  timestamp_at timestamptz not null,
  received_at timestamptz not null,
  severity text not null check (severity in ('trace', 'debug', 'info', 'warning', 'error', 'fatal')),
  body text not null,
  logger text,
  trace_id text,
  span_id text,
  release text,
  environment text,
  resource_attributes jsonb not null default '{}'::jsonb,
  attributes jsonb not null default '{}'::jsonb
);

create index if not exists log_records_project_received_idx
  on log_records(project_id, received_at desc);

create index if not exists log_records_project_severity_received_idx
  on log_records(project_id, severity, received_at desc);

create index if not exists log_records_project_logger_received_idx
  on log_records(project_id, logger, received_at desc)
  where logger is not null;

create index if not exists log_records_project_trace_idx
  on log_records(project_id, trace_id)
  where trace_id is not null;

create index if not exists log_records_resource_attributes_idx
  on log_records using gin (resource_attributes);

create index if not exists log_records_attributes_idx
  on log_records using gin (attributes);

alter table project_hourly_stats
  add column if not exists log_event_count bigint not null default 0 check (log_event_count >= 0);
