alter table telegram_destinations
  add constraint telegram_destinations_chat_id_nonempty
  check (length(btrim(chat_id)) > 0) not valid;

alter table telegram_destinations
  validate constraint telegram_destinations_chat_id_nonempty;

alter table telegram_destinations
  add constraint telegram_destinations_label_nonempty
  check (length(btrim(label)) > 0) not valid;

alter table telegram_destinations
  validate constraint telegram_destinations_label_nonempty;

alter table notification_intents
  add constraint notification_intents_delivered_receipt_present
  check (
    status <> 'delivered'
    or (provider_message_id is not null and delivered_at is not null)
  ) not valid;

alter table notification_intents
  validate constraint notification_intents_delivered_receipt_present;
