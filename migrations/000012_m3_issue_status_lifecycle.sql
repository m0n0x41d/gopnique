alter table issues
  drop constraint if exists issues_status_check;

alter table issues
  add constraint issues_status_check
  check (status in ('unresolved', 'resolved', 'ignored'));

create table if not exists issue_status_transitions (
  id uuid primary key,
  organization_id uuid not null references organizations(id),
  project_id uuid not null references projects(id),
  issue_id uuid not null references issues(id),
  actor_operator_id uuid not null references operators(id),
  from_status text not null check (from_status in ('unresolved', 'resolved', 'ignored')),
  to_status text not null check (to_status in ('unresolved', 'resolved', 'ignored')),
  reason text not null default '',
  created_at timestamptz not null,
  check (from_status <> to_status)
);

create index if not exists issue_status_transitions_issue_created_idx
  on issue_status_transitions(issue_id, created_at desc);
