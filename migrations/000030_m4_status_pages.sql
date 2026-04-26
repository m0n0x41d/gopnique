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
    'status_page_created'
  ));

create table if not exists uptime_status_pages (
  id uuid primary key,
  organization_id uuid not null references organizations(id),
  project_id uuid not null references projects(id),
  name text not null check (length(btrim(name)) between 1 and 128),
  visibility text not null check (visibility in ('private', 'public')),
  public_token text unique,
  enabled boolean not null default true,
  created_by_operator_id uuid references operators(id),
  created_at timestamptz not null,
  updated_at timestamptz not null,
  unique (project_id, name),
  check (
    (
      visibility = 'private'
      and public_token is null
    )
    or
    (
      visibility = 'public'
      and public_token is not null
    )
  )
);

create index if not exists uptime_status_pages_project_created_idx
  on uptime_status_pages(project_id, created_at desc);

create index if not exists uptime_status_pages_public_token_idx
  on uptime_status_pages(public_token)
  where visibility = 'public'
    and enabled = true;
