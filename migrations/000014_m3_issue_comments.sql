alter table audit_events
  drop constraint if exists audit_events_action_check;

alter table audit_events
  add constraint audit_events_action_check
  check (action in (
    'bootstrap',
    'api_token_created',
    'api_token_revoked',
    'issue_comment_created',
    'issue_status_changed'
  ));

create table if not exists issue_comments (
  id uuid primary key,
  organization_id uuid not null references organizations(id),
  project_id uuid not null references projects(id),
  issue_id uuid not null references issues(id),
  actor_operator_id uuid not null references operators(id),
  body text not null check (length(body) between 1 and 4000),
  created_at timestamptz not null
);

create index if not exists issue_comments_issue_created_idx
  on issue_comments(issue_id, created_at asc);
