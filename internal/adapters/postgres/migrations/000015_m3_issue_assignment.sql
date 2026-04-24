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
    'issue_status_changed'
  ));

alter table issues
  add column if not exists assignee_operator_id uuid references operators(id),
  add column if not exists assignee_team_id uuid references teams(id);

alter table issues
  drop constraint if exists issues_single_assignee_check;

alter table issues
  add constraint issues_single_assignee_check
  check (not (assignee_operator_id is not null and assignee_team_id is not null));

create index if not exists issues_project_assignee_operator_idx
  on issues(project_id, assignee_operator_id)
  where assignee_operator_id is not null;

create index if not exists issues_project_assignee_team_idx
  on issues(project_id, assignee_team_id)
  where assignee_team_id is not null;
