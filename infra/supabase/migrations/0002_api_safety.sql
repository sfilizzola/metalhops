-- Close direct Data API access and add idempotency records.

drop policy "genres are public" on genres;
drop policy "verified venues are public" on venues;
drop policy "users submit pending venues" on venues;
drop policy "venue genres follow venue visibility" on venue_genres;

revoke all on table genres from public, anon, authenticated, service_role;
revoke all on table venues from public, anon, authenticated, service_role;
revoke all on table venue_genres from public, anon, authenticated, service_role;
revoke all on sequence genres_id_seq from public, anon, authenticated, service_role;

create table idempotency_records (
  user_id uuid not null references auth.users (id) on delete cascade,
  operation text not null check (operation ~ '^[a-z0-9_]{1,64}$'),
  key text not null check (key ~ '^[ -~]{8,128}$'),
  request_hash text not null check (request_hash ~ '^[0-9a-f]{64}$'),
  response_status smallint not null check (response_status between 200 and 299),
  response_body jsonb not null,
  created_at timestamptz not null default now(),
  expires_at timestamptz not null,
  primary key (user_id, operation, key)
);

create index idempotency_records_expires_at_idx on idempotency_records (expires_at);

alter table idempotency_records enable row level security;

revoke all on table idempotency_records from public, anon, authenticated, service_role;
