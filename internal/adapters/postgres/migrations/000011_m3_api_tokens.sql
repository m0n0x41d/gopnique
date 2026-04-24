create table if not exists api_tokens (
  id uuid primary key,
  organization_id uuid not null references organizations(id),
  project_id uuid not null references projects(id),
  created_by_operator_id uuid not null references operators(id),
  name text not null,
  token_hash bytea not null unique,
  token_prefix text not null,
  scope text not null check (scope in ('project_read', 'project_admin')),
  revoked_at timestamptz,
  last_used_at timestamptz,
  created_at timestamptz not null
);

create index if not exists api_tokens_project_created_idx
  on api_tokens(project_id, created_at desc);

create unique index if not exists api_tokens_project_active_name_idx
  on api_tokens(project_id, name)
  where revoked_at is null;

create index if not exists api_tokens_hash_active_idx
  on api_tokens(token_hash)
  where revoked_at is null;
