create table if not exists zulip_destinations (
  id uuid primary key,
  organization_id uuid not null references organizations(id),
  project_id uuid not null references projects(id),
  label text not null,
  url text not null,
  bot_email text not null,
  api_key text not null,
  stream_name text not null,
  topic_name text not null,
  enabled boolean not null default true,
  created_at timestamptz not null,
  unique (project_id, url, bot_email, stream_name, topic_name)
);

alter table zulip_destinations
  add constraint zulip_destinations_label_nonempty
  check (length(btrim(label)) > 0) not valid;

alter table zulip_destinations
  validate constraint zulip_destinations_label_nonempty;

alter table zulip_destinations
  add constraint zulip_destinations_url_nonempty
  check (length(btrim(url)) > 0) not valid;

alter table zulip_destinations
  validate constraint zulip_destinations_url_nonempty;

alter table zulip_destinations
  add constraint zulip_destinations_bot_email_nonempty
  check (length(btrim(bot_email)) > 0) not valid;

alter table zulip_destinations
  validate constraint zulip_destinations_bot_email_nonempty;

alter table zulip_destinations
  add constraint zulip_destinations_api_key_nonempty
  check (length(btrim(api_key)) > 0) not valid;

alter table zulip_destinations
  validate constraint zulip_destinations_api_key_nonempty;

alter table zulip_destinations
  add constraint zulip_destinations_stream_name_nonempty
  check (length(btrim(stream_name)) > 0) not valid;

alter table zulip_destinations
  validate constraint zulip_destinations_stream_name_nonempty;

alter table zulip_destinations
  add constraint zulip_destinations_topic_name_nonempty
  check (length(btrim(topic_name)) > 0) not valid;

alter table zulip_destinations
  validate constraint zulip_destinations_topic_name_nonempty;

alter table alert_rule_actions
  drop constraint if exists alert_rule_actions_provider_check;

alter table alert_rule_actions
  add constraint alert_rule_actions_provider_check
  check (provider in ('telegram', 'webhook', 'email', 'discord', 'google_chat', 'ntfy', 'microsoft_teams', 'zulip')) not valid;

alter table alert_rule_actions
  validate constraint alert_rule_actions_provider_check;

alter table notification_intents
  drop constraint if exists notification_intents_provider_check;

alter table notification_intents
  add constraint notification_intents_provider_check
  check (provider in ('telegram', 'webhook', 'email', 'discord', 'google_chat', 'ntfy', 'microsoft_teams', 'zulip')) not valid;

alter table notification_intents
  validate constraint notification_intents_provider_check;

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
        or (provider = 'email' and provider_message_id is not null)
        or (provider = 'discord' and provider_status_code is not null)
        or (provider = 'google_chat' and provider_status_code is not null)
        or (provider = 'ntfy' and provider_status_code is not null)
        or (provider = 'microsoft_teams' and provider_status_code is not null)
        or (provider = 'zulip' and provider_status_code is not null)
      )
    )
  ) not valid;

alter table notification_intents
  validate constraint notification_intents_delivered_receipt_present;

create index if not exists notification_intents_zulip_claim_idx
  on notification_intents(provider, status, next_attempt_at, created_at)
  where provider = 'zulip' and status in ('pending', 'failed');
