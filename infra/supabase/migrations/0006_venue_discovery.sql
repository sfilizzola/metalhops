create schema if not exists extensions;
create extension if not exists pg_trgm with schema extensions;

do $$
begin
  if (
    select n.nspname <> 'extensions'
    from pg_extension e
    join pg_namespace n on n.oid = e.extnamespace
    where e.extname = 'pg_trgm'
  ) then
    alter extension pg_trgm set schema extensions;
  end if;
end
$$;

create function venue_search_key(input text) returns text
language sql
stable
set search_path = pg_catalog, public
as $$
  select replace(public.venue_name_key(input), ' ', '')
$$;

revoke execute on function venue_search_key(text) from public, anon, authenticated, service_role;

alter table venues
  add column search_name text,
  add column search_address text,
  add column search_city text;

-- The backfill is not a Venue change, so it must not move updated_at.
alter table venues disable trigger venues_set_updated_at;
update venues set
  search_name = venue_search_key(name),
  search_address = venue_search_key(address),
  search_city = venue_search_key(city);
alter table venues enable trigger venues_set_updated_at;

alter table venues
  alter column search_name set not null,
  alter column search_address set not null,
  alter column search_city set not null;

create function venues_set_search_keys() returns trigger
language plpgsql
set search_path = pg_catalog, public
as $$
begin
  new.search_name = public.venue_search_key(new.name);
  new.search_address = public.venue_search_key(new.address);
  new.search_city = public.venue_search_key(new.city);
  return new;
end
$$;

create trigger venues_set_search_keys
before insert or update on venues
for each row
execute function venues_set_search_keys();

create index venues_search_name_trgm_idx on venues using gin (search_name extensions.gin_trgm_ops);
create index venues_search_address_trgm_idx on venues using gin (search_address extensions.gin_trgm_ops);
create index venues_search_city_trgm_idx on venues using gin (search_city extensions.gin_trgm_ops);
create index venues_search_city_idx on venues (search_city);
create index venues_country_code_idx on venues (country_code);
create index venue_genres_genre_id_idx on venue_genres (genre_id);
