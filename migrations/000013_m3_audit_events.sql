create table if not exists audit_events (
  id uuid primary key,
  organization_id uuid not null references organizations(id),
  project_id uuid references projects(id),
  actor_operator_id uuid references operators(id),
  action text not null check (action in (
    'bootstrap',
    'api_token_created',
    'api_token_revoked',
    'issue_status_changed'
  )),
  target_type text not null,
  target_id text not null,
  metadata jsonb not null default '{}'::jsonb,
  created_at timestamptz not null
);

create index if not exists audit_events_project_created_idx
  on audit_events(project_id, created_at desc)
  where project_id is not null;

create index if not exists audit_events_org_created_idx
  on audit_events(organization_id, created_at desc);
