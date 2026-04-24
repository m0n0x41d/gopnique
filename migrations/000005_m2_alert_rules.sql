create table if not exists alert_rules (
  id uuid primary key,
  organization_id uuid not null references organizations(id),
  project_id uuid not null references projects(id),
  trigger text not null check (trigger = 'issue_opened'),
  name text not null,
  enabled boolean not null default true,
  created_at timestamptz not null,
  unique (project_id, trigger, name)
);

alter table alert_rules
  add constraint alert_rules_name_nonempty
  check (length(btrim(name)) > 0) not valid;

alter table alert_rules
  validate constraint alert_rules_name_nonempty;

create table if not exists alert_rule_actions (
  id uuid primary key,
  organization_id uuid not null references organizations(id),
  project_id uuid not null references projects(id),
  rule_id uuid not null references alert_rules(id),
  provider text not null check (provider = 'telegram'),
  destination_id uuid not null references telegram_destinations(id),
  enabled boolean not null default true,
  created_at timestamptz not null,
  unique (rule_id, provider, destination_id)
);

create index if not exists alert_rules_project_trigger_idx
  on alert_rules(project_id, trigger)
  where enabled = true;

create index if not exists alert_rule_actions_rule_provider_idx
  on alert_rule_actions(rule_id, provider)
  where enabled = true;
