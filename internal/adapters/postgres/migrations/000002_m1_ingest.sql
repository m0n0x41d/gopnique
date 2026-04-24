create table if not exists organizations (
  id uuid primary key,
  slug text not null unique,
  name text not null,
  accepting_events boolean not null default true,
  created_at timestamptz not null
);

create table if not exists operators (
  id uuid primary key,
  email text not null unique,
  display_name text not null,
  password_hash text not null,
  created_at timestamptz not null
);

create table if not exists operator_organizations (
  operator_id uuid not null references operators(id),
  organization_id uuid not null references organizations(id),
  role text not null check (role = 'owner'),
  created_at timestamptz not null,
  primary key (operator_id, organization_id)
);

create table if not exists projects (
  id uuid primary key,
  organization_id uuid not null references organizations(id),
  ingest_ref text not null unique,
  slug text not null,
  name text not null,
  accepting_events boolean not null default true,
  scrub_ip_addresses boolean not null default true,
  first_event_at timestamptz,
  next_issue_short_id bigint not null default 1,
  created_at timestamptz not null,
  unique (organization_id, slug)
);

create table if not exists project_keys (
  id uuid primary key,
  project_id uuid not null references projects(id),
  public_key uuid not null unique,
  label text not null,
  active boolean not null default true,
  created_at timestamptz not null
);

create table if not exists events (
  id uuid primary key,
  organization_id uuid not null references organizations(id),
  project_id uuid not null references projects(id),
  event_id uuid not null,
  kind text not null check (kind in ('error', 'default', 'transaction')),
  level text,
  title text not null,
  platform text not null,
  occurred_at timestamptz not null,
  received_at timestamptz not null,
  release text,
  environment text,
  transaction_name text,
  fingerprint text not null,
  canonical_payload jsonb,
  protocol_observations jsonb not null default '[]'::jsonb,
  unique (project_id, event_id)
);

create index if not exists events_project_received_idx
  on events(project_id, received_at desc);

create index if not exists events_project_fingerprint_idx
  on events(project_id, fingerprint);

create index if not exists events_project_release_idx
  on events(project_id, release)
  where release is not null;

create index if not exists events_project_environment_idx
  on events(project_id, environment)
  where environment is not null;

create table if not exists issues (
  id uuid primary key,
  organization_id uuid not null references organizations(id),
  project_id uuid not null references projects(id),
  short_id bigint not null,
  type text not null check (type in ('error', 'default')),
  status text not null check (status = 'unresolved'),
  title text not null,
  first_seen_at timestamptz not null,
  last_seen_at timestamptz not null,
  event_count bigint not null check (event_count >= 1),
  last_event_id uuid not null references events(id),
  release text,
  environment text,
  created_at timestamptz not null,
  unique (project_id, short_id)
);

create index if not exists issues_project_last_seen_idx
  on issues(project_id, last_seen_at desc);

create index if not exists issues_project_status_idx
  on issues(project_id, status, last_seen_at desc);

create table if not exists issue_fingerprints (
  project_id uuid not null references projects(id),
  fingerprint text not null,
  issue_id uuid not null references issues(id),
  created_at timestamptz not null,
  primary key (project_id, fingerprint)
);

create table if not exists event_tags (
  event_id uuid not null references events(id) on delete cascade,
  project_id uuid not null references projects(id),
  key text not null,
  value text not null,
  primary key (event_id, key)
);

create index if not exists event_tags_project_key_value_idx
  on event_tags(project_id, key, value);

create table if not exists operator_sessions (
  id uuid primary key,
  operator_id uuid not null references operators(id),
  token_hash bytea not null unique,
  expires_at timestamptz not null,
  created_at timestamptz not null
);

