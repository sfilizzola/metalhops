-- Review safety: commentary removal marker, Review Reports, moderation audit, Commentary restrictions.
alter table reviews
  add column commentary_removed_at timestamptz;

alter table reviews
  add constraint reviews_commentary_removed_check check (
    commentary_removed_at is null or commentary is null
  );

create table review_reports (
  id uuid primary key default gen_random_uuid(),
  reporter_user_id uuid not null references auth.users (id) on delete cascade,
  review_id uuid not null references reviews (id) on delete cascade,
  reason text not null,
  note text,
  status text not null default 'pending',
  created_at timestamptz not null default now(),
  resolved_at timestamptz,
  resolved_by_user_id uuid references auth.users (id) on delete set null,
  constraint review_reports_reason_check check (
    reason in (
      'spam',
      'harassment',
      'hate_or_discrimination',
      'personal_information',
      'illegal_content',
      'off_topic',
      'other'
    )
  ),
  constraint review_reports_status_check check (
    status in ('pending', 'actioned', 'dismissed', 'author_resolved')
  ),
  constraint review_reports_note_length check (
    note is null or char_length(note) between 1 and 1000
  ),
  constraint review_reports_note_required check (
    reason <> 'other' or note is not null
  ),
  constraint review_reports_resolution_check check (
    (status = 'pending' and resolved_at is null and resolved_by_user_id is null)
    or (status in ('actioned', 'dismissed', 'author_resolved') and resolved_at is not null)
  )
);

create unique index review_reports_pending_pair_key
  on review_reports (reporter_user_id, review_id)
  where status = 'pending';

create index review_reports_pending_list_key
  on review_reports (created_at, id)
  where status = 'pending';

create index review_reports_pending_review_key
  on review_reports (review_id)
  where status = 'pending';

create table review_moderation_actions (
  id uuid primary key default gen_random_uuid(),
  review_id uuid not null,
  reviewer_user_id uuid references auth.users (id) on delete set null,
  venue_id uuid not null references venues (id),
  action text not null,
  reason text not null,
  note text,
  operator_user_id uuid references auth.users (id) on delete set null,
  created_at timestamptz not null default now(),
  constraint review_moderation_actions_action_check check (
    action in ('commentary_removed', 'review_deleted')
  ),
  constraint review_moderation_actions_reason_check check (
    reason in (
      'spam',
      'harassment',
      'hate_or_discrimination',
      'personal_information',
      'illegal_content',
      'off_topic',
      'other'
    )
  ),
  constraint review_moderation_actions_note_check check (
    (note is null or char_length(note) between 1 and 1000)
    and (reason is distinct from 'other' or note is not null)
  )
);

create index review_moderation_actions_review_key
  on review_moderation_actions (review_id, created_at desc, id desc);

create function reject_review_moderation_action_mutation() returns trigger
language plpgsql as $$
begin
  -- ON DELETE SET NULL updates this row and fires the BEFORE UPDATE trigger.
  -- Nulling reviewer_user_id or operator_user_id is the only change allowed.
  if tg_op = 'UPDATE'
    and new.id is not distinct from old.id
    and new.review_id is not distinct from old.review_id
    and new.venue_id is not distinct from old.venue_id
    and new.action is not distinct from old.action
    and new.reason is not distinct from old.reason
    and new.note is not distinct from old.note
    and new.created_at is not distinct from old.created_at
    and (new.reviewer_user_id is not distinct from old.reviewer_user_id or new.reviewer_user_id is null)
    and (new.operator_user_id is not distinct from old.operator_user_id or new.operator_user_id is null)
  then
    return new;
  end if;
  raise exception 'review moderation actions are append-only' using errcode = '55000';
end $$;

create trigger review_moderation_actions_append_only
  before update or delete on review_moderation_actions
  for each row execute function reject_review_moderation_action_mutation();

create function reject_review_moderation_action_without_actor() returns trigger
language plpgsql as $$
begin
  if new.operator_user_id is null then
    raise exception 'review moderation actions require an operator id' using errcode = '23514';
  end if;
  return new;
end $$;

create trigger review_moderation_actions_actor_required
  before insert on review_moderation_actions
  for each row execute function reject_review_moderation_action_without_actor();

create table review_commentary_restrictions (
  user_id uuid not null references auth.users (id) on delete cascade,
  venue_id uuid not null references venues (id) on delete cascade,
  source_action_id uuid not null references review_moderation_actions (id),
  created_at timestamptz not null default now(),
  constraint review_commentary_restrictions_pair_key unique (user_id, venue_id)
);

alter table review_reports enable row level security;
alter table review_moderation_actions enable row level security;
alter table review_commentary_restrictions enable row level security;
revoke all on table review_reports from public, anon, authenticated, service_role;
revoke all on table review_moderation_actions from public, anon, authenticated, service_role;
revoke all on table review_commentary_restrictions from public, anon, authenticated, service_role;
revoke execute on function reject_review_moderation_action_mutation() from public, anon, authenticated, service_role;
revoke execute on function reject_review_moderation_action_without_actor() from public, anon, authenticated, service_role;
