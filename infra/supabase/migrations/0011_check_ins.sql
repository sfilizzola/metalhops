-- Check-ins: one row per recorded User visit to a Venue.
create table check_ins (
  id uuid primary key default gen_random_uuid(),
  venue_id uuid not null references venues (id) on delete cascade,
  user_id uuid not null references auth.users (id) on delete cascade,
  visited_at timestamptz not null,
  recorded_at timestamptz not null
);

create index check_ins_user_history_idx on check_ins (user_id, visited_at desc, id desc);
create index check_ins_user_venue_visit_idx on check_ins (user_id, venue_id, visited_at);
create index check_ins_venue_idx on check_ins (venue_id);
create index check_ins_venue_feed_idx on check_ins (venue_id, recorded_at desc, id desc);

alter table check_ins enable row level security;
revoke all on table check_ins from public, anon, authenticated, service_role;
