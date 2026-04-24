create table if not exists webhook_destinations (
  id uuid primary key,
  organization_id uuid not null references organizations(id),
  project_id uuid not null references projects(id),
  label text not null,
  url text not null,
  enabled boolean not null default true,
  created_at timestamptz not null,
  unique (project_id, url)
);

alter table webhook_destinations
  add constraint webhook_destinations_label_nonempty
  check (length(btrim(label)) > 0) not valid;

alter table webhook_destinations
  validate constraint webhook_destinations_label_nonempty;

alter table webhook_destinations
  add constraint webhook_destinations_url_nonempty
  check (length(btrim(url)) > 0) not valid;

alter table webhook_destinations
  validate constraint webhook_destinations_url_nonempty;

alter table alert_rule_actions
  drop constraint if exists alert_rule_actions_provider_check;

alter table alert_rule_actions
  drop constraint if exists alert_rule_actions_destination_id_fkey;

alter table alert_rule_actions
  add constraint alert_rule_actions_provider_check
  check (provider in ('telegram', 'webhook')) not valid;

alter table alert_rule_actions
  validate constraint alert_rule_actions_provider_check;

alter table notification_intents
  drop constraint if exists notification_intents_provider_check;

alter table notification_intents
  drop constraint if exists notification_intents_destination_id_fkey;

alter table notification_intents
  add constraint notification_intents_provider_check
  check (provider in ('telegram', 'webhook')) not valid;

alter table notification_intents
  validate constraint notification_intents_provider_check;

alter table notification_intents
  add column if not exists provider_status_code integer;

alter table notification_intents
  drop constraint if exists notification_intents_delivered_receipt_present;

alter table notification_intents
  add constraint notification_intents_delivered_receipt_present
  check (
    status <> 'delivered'
    or (
      delivered_at is not null
      and (
        (provider = 'telegram' and provider_message_id is not null)
        or (provider = 'webhook' and provider_status_code is not null)
      )
    )
  ) not valid;

alter table notification_intents
  validate constraint notification_intents_delivered_receipt_present;

create index if not exists notification_intents_webhook_claim_idx
  on notification_intents(provider, status, next_attempt_at, created_at)
  where provider = 'webhook' and status in ('pending', 'failed');
