create table platform_operators (
  user_id uuid primary key references auth.users (id) on delete cascade,
  created_at timestamptz not null default now()
);

create table profile_reports (
  id uuid primary key default gen_random_uuid(),
  reporter_user_id uuid not null references auth.users (id) on delete cascade,
  target_user_id uuid not null references auth.users (id) on delete cascade,
  reason text not null,
  note text,
  status text not null default 'pending',
  created_at timestamptz not null default now(),
  resolved_at timestamptz,
  resolved_by_user_id uuid references auth.users (id) on delete set null,
  constraint profile_reports_reason_check check (
    reason in ('abusive_name', 'hate_or_discrimination', 'impersonation', 'personal_information', 'spam', 'other')
  ),
  constraint profile_reports_status_check check (
    status in ('pending', 'actioned', 'dismissed')
  ),
  constraint profile_reports_note_length check (
    note is null or char_length(note) between 1 and 1000
  ),
  constraint profile_reports_note_required check (
    reason not in ('impersonation', 'personal_information', 'other') or note is not null
  ),
  constraint profile_reports_distinct_users check (reporter_user_id <> target_user_id),
  constraint profile_reports_resolution_check check (
    (status = 'pending' and resolved_at is null and resolved_by_user_id is null)
    or (status in ('actioned', 'dismissed') and resolved_at is not null)
  )
);

create unique index profile_reports_pending_pair_key
  on profile_reports (reporter_user_id, target_user_id)
  where status = 'pending';

create index profile_reports_pending_list_key
  on profile_reports (created_at, id)
  where status = 'pending';

create table report_quota_events (
  id uuid primary key default gen_random_uuid(),
  user_id uuid not null references auth.users (id) on delete cascade,
  created_at timestamptz not null default now()
);

create index report_quota_events_user_created_key
  on report_quota_events (user_id, created_at);

create table profile_moderation_actions (
  id uuid primary key default gen_random_uuid(),
  subject_user_id uuid references auth.users (id) on delete set null,
  action text not null,
  actor_type text not null,
  actor_user_id uuid references auth.users (id) on delete set null,
  reason text,
  note text,
  suspended_until timestamptz,
  created_at timestamptz not null default now(),
  constraint profile_moderation_actions_action_check check (
    action in ('rename_required', 'suspended', 'restored')
  ),
  constraint profile_moderation_actions_actor_check check (
    actor_type in ('operator', 'user', 'system')
    and (actor_type <> 'system' or actor_user_id is null)
  ),
  constraint profile_moderation_actions_reason_check check (
    (action = 'restored' and (reason is null or reason in ('abusive_name', 'hate_or_discrimination', 'impersonation', 'personal_information', 'spam', 'other')))
    or (action in ('rename_required', 'suspended') and reason in ('abusive_name', 'hate_or_discrimination', 'impersonation', 'personal_information', 'spam', 'other'))
  ),
  constraint profile_moderation_actions_expiry_check check (
    action = 'suspended' or suspended_until is null
  ),
  constraint profile_moderation_actions_note_check check (
    (note is null or char_length(note) between 1 and 1000)
    and (reason is distinct from 'other' or note is not null)
  )
);

create function reject_overlapping_identity() returns trigger
language plpgsql as $$
begin
  if tg_table_name = 'platform_operators' then
    if exists (select 1 from profiles where id = new.user_id) then
      raise exception 'operator and profile overlap' using errcode = '23514';
    end if;
  elsif exists (select 1 from platform_operators where user_id = new.id) then
    raise exception 'operator and profile overlap' using errcode = '23514';
  end if;
  return new;
end $$;

create trigger platform_operators_no_profile
  before insert or update on platform_operators
  for each row execute function reject_overlapping_identity();

create trigger profiles_no_operator
  before insert or update on profiles
  for each row execute function reject_overlapping_identity();

create function reject_moderation_action_mutation() returns trigger
language plpgsql as $$
begin
  -- ON DELETE SET NULL updates this row and fires the BEFORE UPDATE trigger.
  -- Nulling subject_user_id and/or actor_user_id is the only change allowed.
  if tg_op = 'UPDATE'
    and new.id is not distinct from old.id
    and new.action is not distinct from old.action
    and new.actor_type is not distinct from old.actor_type
    and new.reason is not distinct from old.reason
    and new.note is not distinct from old.note
    and new.suspended_until is not distinct from old.suspended_until
    and new.created_at is not distinct from old.created_at
    and (new.subject_user_id is not distinct from old.subject_user_id or new.subject_user_id is null)
    and (new.actor_user_id is not distinct from old.actor_user_id or new.actor_user_id is null)
  then
    return new;
  end if;
  raise exception 'profile moderation actions are append-only' using errcode = '55000';
end $$;

create trigger profile_moderation_actions_append_only
  before update or delete on profile_moderation_actions
  for each row execute function reject_moderation_action_mutation();

create function reject_moderation_action_without_actor() returns trigger
language plpgsql as $$
begin
  if new.actor_type in ('operator', 'user') and new.actor_user_id is null then
    raise exception 'operator and user actors require an actor id' using errcode = '23514';
  end if;
  return new;
end $$;

create trigger profile_moderation_actions_actor_required
  before insert on profile_moderation_actions
  for each row execute function reject_moderation_action_without_actor();

alter table profiles add constraint profiles_moderation_reason_check check (
  moderation_reason is null or moderation_reason in (
    'abusive_name', 'hate_or_discrimination', 'impersonation', 'personal_information', 'spam', 'other'
  )
);
alter table profiles add constraint profiles_rename_required_check check (
  status <> 'rename_required' or (moderation_reason is not null and suspended_until is null)
);
alter table profiles add constraint profiles_suspended_check check (
  status <> 'suspended' or moderation_reason is not null
);
alter table profiles add constraint profiles_moderation_note_present_check check (
  moderation_note is null or char_length(moderation_note) >= 1
);
alter table profiles add constraint profiles_other_note_check check (
  moderation_reason is distinct from 'other' or moderation_note is not null
);

alter table platform_operators enable row level security;
alter table profile_reports enable row level security;
alter table report_quota_events enable row level security;
alter table profile_moderation_actions enable row level security;
revoke all on table platform_operators from public, anon, authenticated, service_role;
revoke all on table profile_reports from public, anon, authenticated, service_role;
revoke all on table report_quota_events from public, anon, authenticated, service_role;
revoke all on table profile_moderation_actions from public, anon, authenticated, service_role;
