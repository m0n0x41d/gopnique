create table if not exists telegram_destinations (
  id uuid primary key,
  organization_id uuid not null references organizations(id),
  project_id uuid not null references projects(id),
  label text not null,
  chat_id text not null,
  enabled boolean not null default true,
  created_at timestamptz not null,
  unique (project_id, chat_id)
);

create table if not exists notification_intents (
  id uuid primary key,
  organization_id uuid not null references organizations(id),
  project_id uuid not null references projects(id),
  issue_id uuid not null references issues(id),
  event_id uuid not null references events(id),
  provider text not null check (provider = 'telegram'),
  destination_id uuid not null references telegram_destinations(id),
  status text not null check (status in ('pending', 'delivering', 'delivered', 'failed')),
  dedupe_key text not null unique,
  attempts integer not null default 0 check (attempts >= 0),
  next_attempt_at timestamptz not null,
  locked_until timestamptz,
  provider_message_id text,
  last_error text,
  created_at timestamptz not null,
  delivered_at timestamptz
);

create index if not exists notification_intents_telegram_claim_idx
  on notification_intents(provider, status, next_attempt_at, created_at)
  where provider = 'telegram' and status in ('pending', 'failed');

create index if not exists notification_intents_project_issue_idx
  on notification_intents(project_id, issue_id);
