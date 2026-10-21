-- Reviews: one rating and optional commentary per User and Venue.
create table reviews (
  id uuid primary key default gen_random_uuid(),
  venue_id uuid not null references venues (id) on delete cascade,
  user_id uuid not null references auth.users (id) on delete cascade,
  rating integer not null,
  commentary text,
  created_at timestamptz not null,
  updated_at timestamptz not null,
  constraint reviews_rating_check check (rating between 1 and 5),
  constraint reviews_commentary_check check (commentary is null or char_length(commentary) between 1 and 2000),
  constraint reviews_venue_user_key unique (venue_id, user_id)
);

create index reviews_venue_list_idx on reviews (venue_id, created_at desc, id desc);

alter table reviews enable row level security;
revoke all on table reviews from public, anon, authenticated, service_role;
