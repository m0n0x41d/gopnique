-- Canonical copy lives in internal/adapters/postgres/migrations for one-binary embedding.
-- This root file is kept as an operator-visible migration carrier.

create table if not exists transaction_events (
  event_id uuid primary key references events(id) on delete cascade,
  organization_id uuid not null references organizations(id),
  project_id uuid not null references projects(id),
  transaction_name text not null,
  operation text not null,
  duration_ms double precision not null check (duration_ms >= 0),
  status text not null,
  trace_id text,
  span_id text,
  parent_span_id text,
  span_count integer not null default 0 check (span_count >= 0),
  spans jsonb not null default '[]'::jsonb,
  occurred_at timestamptz not null,
  received_at timestamptz not null
);

create index if not exists transaction_events_project_received_idx
  on transaction_events(project_id, received_at desc);

create index if not exists transaction_events_project_name_operation_idx
  on transaction_events(project_id, transaction_name, operation);

create index if not exists transaction_events_project_trace_idx
  on transaction_events(project_id, trace_id)
  where trace_id is not null;
