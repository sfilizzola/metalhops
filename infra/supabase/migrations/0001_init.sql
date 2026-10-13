-- MetalHops initial schema
create extension if not exists postgis;

create table genres (
  id   serial primary key,
  name text not null unique
);

insert into genres (name) values
  ('Classic rock'), ('Hard rock'), ('Heavy metal'), ('Death metal'),
  ('Black metal'), ('Thrash metal'), ('Folk metal'), ('Punk'), ('Grunge');

create table venues (
  id                  uuid primary key default gen_random_uuid(),
  name                text not null,
  description         text,
  location            geography(Point, 4326) not null,
  address             text,
  city                text,
  country             text,
  venue_type          text not null default 'bar' check (venue_type in ('bar', 'pub', 'club')),
  verification_status text not null default 'pending'
                      check (verification_status in ('pending', 'verified', 'rejected')),
  submitted_by        uuid references auth.users (id),
  created_at          timestamptz not null default now()
);

create index venues_location_idx on venues using gist (location);

create table venue_genres (
  venue_id uuid references venues (id) on delete cascade,
  genre_id int  references genres (id) on delete cascade,
  primary key (venue_id, genre_id)
);

-- Row-level security
alter table genres       enable row level security;
alter table venues       enable row level security;
alter table venue_genres enable row level security;

create policy "genres are public" on genres
  for select using (true);

create policy "verified venues are public" on venues
  for select using (verification_status = 'verified' or submitted_by = auth.uid());

create policy "users submit pending venues" on venues
  for insert to authenticated
  with check (submitted_by = auth.uid() and verification_status = 'pending');

create policy "venue genres follow venue visibility" on venue_genres
  for select using (
    exists (select 1 from venues v where v.id = venue_id
            and (v.verification_status = 'verified' or v.submitted_by = auth.uid()))
  );
