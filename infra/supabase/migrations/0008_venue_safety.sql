-- Venue safety: delisting, rejection feedback, Venue Reports, and the moderation audit log.
alter table venues drop constraint venues_verification_status_check;
alter table venues add constraint venues_verification_status_check
  check (verification_status in ('pending', 'verified', 'rejected', 'delisted'));

alter table venues
  add column rejection_reason text,
  add column rejection_note text,
  add column delisting_reason text,
  add column delisting_note text;

-- Rows rejected before feedback existed need a reason for the state check below.
update venues
set rejection_reason = 'other', rejection_note = 'Rejected before rejection feedback existed.'
where verification_status = 'rejected';

alter table venues
  add constraint venues_rejection_reason_check check (
    rejection_reason is null
    or rejection_reason in ('duplicate', 'not_eligible', 'inaccurate_details', 'insufficient_information', 'cannot_verify', 'other')
  ),
  add constraint venues_delisting_reason_check check (
    delisting_reason is null
    or delisting_reason in ('permanently_closed', 'duplicate', 'no_longer_eligible', 'other')
  ),
  add constraint venues_rejection_state_check check (
    (verification_status = 'rejected') = (rejection_reason is not null)
    and (rejection_note is null or rejection_reason is not null)
  ),
  add constraint venues_delisting_state_check check (
    (verification_status = 'delisted') = (delisting_reason is not null)
    and (delisting_note is null or delisting_reason is not null)
  ),
  add constraint venues_rejection_note_check check (
    rejection_note is null or char_length(rejection_note) between 1 and 1000
  ),
  add constraint venues_delisting_note_check check (
    delisting_note is null or char_length(delisting_note) between 1 and 1000
  ),
  add constraint venues_rejection_other_note_check check (
    rejection_reason is distinct from 'other' or rejection_note is not null
  ),
  add constraint venues_delisting_other_note_check check (
    delisting_reason is distinct from 'other' or delisting_note is not null
  );

create table venue_reports (
  id uuid primary key default gen_random_uuid(),
  reporter_user_id uuid not null references auth.users (id) on delete cascade,
  venue_id uuid not null references venues (id) on delete cascade,
  reason text not null,
  note text,
  status text not null default 'pending',
  created_at timestamptz not null default now(),
  resolved_at timestamptz,
  resolved_by_user_id uuid references auth.users (id) on delete set null,
  constraint venue_reports_reason_check check (
    reason in ('permanently_closed', 'moved', 'duplicate', 'incorrect_details', 'no_longer_eligible', 'other')
  ),
  constraint venue_reports_status_check check (status in ('pending', 'actioned', 'dismissed')),
  constraint venue_reports_note_length check (note is null or char_length(note) between 1 and 1000),
  constraint venue_reports_note_required check (
    reason not in ('incorrect_details', 'other') or note is not null
  ),
  constraint venue_reports_resolution_check check (
    (status = 'pending' and resolved_at is null and resolved_by_user_id is null)
    or (status in ('actioned', 'dismissed') and resolved_at is not null)
  )
);

create unique index venue_reports_pending_pair_key
  on venue_reports (reporter_user_id, venue_id)
  where status = 'pending';

create index venue_reports_pending_list_key
  on venue_reports (created_at, id)
  where status = 'pending';

create index venue_reports_pending_venue_key
  on venue_reports (venue_id, created_at, id)
  where status = 'pending';

create table venue_moderation_actions (
  id uuid primary key default gen_random_uuid(),
  venue_id uuid not null references venues (id),
  action text not null,
  prior_status text not null,
  new_status text not null,
  rejection_reason text,
  rejection_note text,
  delisting_reason text,
  delisting_note text,
  actor_user_id uuid references auth.users (id) on delete set null,
  created_at timestamptz not null default now(),
  constraint venue_moderation_actions_transition_check check (
    (action = 'verify' and prior_status = 'pending' and new_status = 'verified')
    or (action = 'reject' and prior_status = 'pending' and new_status = 'rejected')
    or (action = 'delist' and prior_status = 'verified' and new_status = 'delisted')
    or (action = 'reopen' and prior_status = 'delisted' and new_status = 'verified')
    or (action = 'correct' and prior_status = new_status and new_status in ('pending', 'verified'))
  ),
  constraint venue_moderation_actions_reason_check check (
    ((rejection_reason is not null) = (action = 'reject'))
    and ((delisting_reason is not null) = (action = 'delist'))
    and (rejection_reason is null or rejection_reason in ('duplicate', 'not_eligible', 'inaccurate_details', 'insufficient_information', 'cannot_verify', 'other'))
    and (delisting_reason is null or delisting_reason in ('permanently_closed', 'duplicate', 'no_longer_eligible', 'other'))
  ),
  constraint venue_moderation_actions_note_check check (
    (rejection_note is null or (rejection_reason is not null and char_length(rejection_note) between 1 and 1000))
    and (delisting_note is null or (delisting_reason is not null and char_length(delisting_note) between 1 and 1000))
    and (rejection_reason is distinct from 'other' or rejection_note is not null)
    and (delisting_reason is distinct from 'other' or delisting_note is not null)
  )
);

create index venue_moderation_actions_venue_key
  on venue_moderation_actions (venue_id, created_at, id);

create function reject_venue_moderation_action_mutation() returns trigger
language plpgsql as $$
begin
  -- ON DELETE SET NULL updates this row and fires the BEFORE UPDATE trigger.
  -- Nulling actor_user_id is the only change allowed.
  if tg_op = 'UPDATE'
    and new.id is not distinct from old.id
    and new.venue_id is not distinct from old.venue_id
    and new.action is not distinct from old.action
    and new.prior_status is not distinct from old.prior_status
    and new.new_status is not distinct from old.new_status
    and new.rejection_reason is not distinct from old.rejection_reason
    and new.rejection_note is not distinct from old.rejection_note
    and new.delisting_reason is not distinct from old.delisting_reason
    and new.delisting_note is not distinct from old.delisting_note
    and new.created_at is not distinct from old.created_at
    and (new.actor_user_id is not distinct from old.actor_user_id or new.actor_user_id is null)
  then
    return new;
  end if;
  raise exception 'venue moderation actions are append-only' using errcode = '55000';
end $$;

create trigger venue_moderation_actions_append_only
  before update or delete on venue_moderation_actions
  for each row execute function reject_venue_moderation_action_mutation();

create function reject_venue_moderation_action_without_actor() returns trigger
language plpgsql as $$
begin
  if new.actor_user_id is null then
    raise exception 'venue moderation actions require an actor id' using errcode = '23514';
  end if;
  return new;
end $$;

create trigger venue_moderation_actions_actor_required
  before insert on venue_moderation_actions
  for each row execute function reject_venue_moderation_action_without_actor();

alter table venue_reports enable row level security;
alter table venue_moderation_actions enable row level security;
revoke all on table venue_reports from public, anon, authenticated, service_role;
revoke all on table venue_moderation_actions from public, anon, authenticated, service_role;
revoke execute on function reject_venue_moderation_action_mutation() from public, anon, authenticated, service_role;
revoke execute on function reject_venue_moderation_action_without_actor() from public, anon, authenticated, service_role;
