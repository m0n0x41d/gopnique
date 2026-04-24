alter table operators
  add column if not exists active boolean not null default true;

create index if not exists operators_active_email_idx
  on operators(email)
  where active = true;
