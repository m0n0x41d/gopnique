create table if not exists oauth_oidc_providers (
  id uuid primary key,
  slug text not null unique check (slug ~ '^[a-z0-9][a-z0-9-]{0,62}$'),
  display_name text not null check (length(btrim(display_name)) between 1 and 128),
  issuer text not null,
  client_id text not null,
  client_secret text not null default '',
  authorization_endpoint text not null,
  token_endpoint text not null,
  userinfo_endpoint text not null,
  scopes text not null,
  enabled boolean not null default true,
  created_at timestamptz not null,
  updated_at timestamptz not null
);

create index if not exists oauth_oidc_providers_enabled_idx
  on oauth_oidc_providers(slug)
  where enabled = true;

create table if not exists oauth_login_states (
  state_hash bytea primary key,
  provider_id uuid not null references oauth_oidc_providers(id) on delete cascade,
  code_verifier text not null check (length(btrim(code_verifier)) between 32 and 256),
  redirect_path text not null default '/issues' check (left(redirect_path, 1) = '/'),
  expires_at timestamptz not null,
  consumed_at timestamptz,
  created_at timestamptz not null,
  check (consumed_at is null or consumed_at >= created_at)
);

create index if not exists oauth_login_states_provider_expires_idx
  on oauth_login_states(provider_id, expires_at);

create table if not exists operator_external_identities (
  provider_id uuid not null references oauth_oidc_providers(id) on delete cascade,
  subject text not null check (length(btrim(subject)) between 1 and 512),
  operator_id uuid not null references operators(id),
  email text not null check (position('@' in email) > 1),
  created_at timestamptz not null,
  last_login_at timestamptz not null,
  primary key (provider_id, subject),
  unique (provider_id, operator_id)
);

create index if not exists operator_external_identities_operator_idx
  on operator_external_identities(operator_id);
