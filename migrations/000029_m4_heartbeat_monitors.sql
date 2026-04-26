alter table uptime_monitors
  alter column target_url drop not null;

alter table uptime_monitors
  add column if not exists heartbeat_endpoint_id text;

alter table uptime_monitors
  add column if not exists heartbeat_grace_seconds integer;

alter table uptime_monitors
  add column if not exists last_check_in_at timestamptz;

alter table uptime_monitors
  drop constraint if exists uptime_monitors_monitor_type_check;

alter table uptime_monitors
  add constraint uptime_monitors_monitor_type_check
  check (monitor_type in ('http', 'heartbeat'));

alter table uptime_monitors
  drop constraint if exists uptime_monitors_kind_shape_check;

alter table uptime_monitors
  add constraint uptime_monitors_kind_shape_check
  check (
    (
      monitor_type = 'http'
      and target_url is not null
      and heartbeat_endpoint_id is null
      and heartbeat_grace_seconds is null
    )
    or
    (
      monitor_type = 'heartbeat'
      and target_url is null
      and heartbeat_endpoint_id is not null
      and heartbeat_grace_seconds between 60 and 86400
    )
  );

create unique index if not exists uptime_monitors_heartbeat_endpoint_idx
  on uptime_monitors(heartbeat_endpoint_id)
  where monitor_type = 'heartbeat';
