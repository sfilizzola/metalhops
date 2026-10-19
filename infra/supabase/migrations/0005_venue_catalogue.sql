alter table venues rename column country to country_code;

alter table venues add column updated_at timestamptz;
update venues set updated_at = created_at;
alter table venues alter column updated_at set not null;
alter table venues alter column updated_at set default now();

alter table venues alter column address set not null;
alter table venues alter column city set not null;
alter table venues alter column country_code set not null;

alter table venues add constraint venues_name_check check (
  char_length(name) between 1 and 150
  and name = btrim(name)
  and name !~ E'[\n\r]'
);

alter table venues add constraint venues_address_check check (
  char_length(address) between 1 and 500
  and address = btrim(address)
  and address !~ E'[\n\r]'
);

alter table venues add constraint venues_city_check check (
  char_length(city) between 1 and 120
  and city = btrim(city)
  and city !~ E'[\n\r]'
);

alter table venues add constraint venues_description_check check (
  description is null
  or (
    char_length(description) between 1 and 2000
    and description = btrim(description)
  )
);

alter table venues add constraint venues_country_code_check check (country_code in (
  'AD', 'AE', 'AF', 'AG', 'AI', 'AL', 'AM', 'AO', 'AQ', 'AR',
  'AS', 'AT', 'AU', 'AW', 'AX', 'AZ', 'BA', 'BB', 'BD', 'BE',
  'BF', 'BG', 'BH', 'BI', 'BJ', 'BL', 'BM', 'BN', 'BO', 'BQ',
  'BR', 'BS', 'BT', 'BV', 'BW', 'BY', 'BZ', 'CA', 'CC', 'CD',
  'CF', 'CG', 'CH', 'CI', 'CK', 'CL', 'CM', 'CN', 'CO', 'CR',
  'CU', 'CV', 'CW', 'CX', 'CY', 'CZ', 'DE', 'DJ', 'DK', 'DM',
  'DO', 'DZ', 'EC', 'EE', 'EG', 'EH', 'ER', 'ES', 'ET', 'FI',
  'FJ', 'FK', 'FM', 'FO', 'FR', 'GA', 'GB', 'GD', 'GE', 'GF',
  'GG', 'GH', 'GI', 'GL', 'GM', 'GN', 'GP', 'GQ', 'GR', 'GS',
  'GT', 'GU', 'GW', 'GY', 'HK', 'HM', 'HN', 'HR', 'HT', 'HU',
  'ID', 'IE', 'IL', 'IM', 'IN', 'IO', 'IQ', 'IR', 'IS', 'IT',
  'JE', 'JM', 'JO', 'JP', 'KE', 'KG', 'KH', 'KI', 'KM', 'KN',
  'KP', 'KR', 'KW', 'KY', 'KZ', 'LA', 'LB', 'LC', 'LI', 'LK',
  'LR', 'LS', 'LT', 'LU', 'LV', 'LY', 'MA', 'MC', 'MD', 'ME',
  'MF', 'MG', 'MH', 'MK', 'ML', 'MM', 'MN', 'MO', 'MP', 'MQ',
  'MR', 'MS', 'MT', 'MU', 'MV', 'MW', 'MX', 'MY', 'MZ', 'NA',
  'NC', 'NE', 'NF', 'NG', 'NI', 'NL', 'NO', 'NP', 'NR', 'NU',
  'NZ', 'OM', 'PA', 'PE', 'PF', 'PG', 'PH', 'PK', 'PL', 'PM',
  'PN', 'PR', 'PS', 'PT', 'PW', 'PY', 'QA', 'RE', 'RO', 'RS',
  'RU', 'RW', 'SA', 'SB', 'SC', 'SD', 'SE', 'SG', 'SH', 'SI',
  'SJ', 'SK', 'SL', 'SM', 'SN', 'SO', 'SR', 'SS', 'ST', 'SV',
  'SX', 'SY', 'SZ', 'TC', 'TD', 'TF', 'TG', 'TH', 'TJ', 'TK',
  'TL', 'TM', 'TN', 'TO', 'TR', 'TT', 'TV', 'TW', 'TZ', 'UA',
  'UG', 'UM', 'US', 'UY', 'UZ', 'VA', 'VC', 'VE', 'VG', 'VI',
  'VN', 'VU', 'WF', 'WS', 'XK', 'YE', 'YT', 'ZA', 'ZM', 'ZW'
));

create or replace function venues_set_updated_at() returns trigger
language plpgsql
as $$
begin
  new.updated_at = now();
  return new;
end
$$;

create trigger venues_set_updated_at
before update on venues
for each row
execute function venues_set_updated_at();

create schema if not exists extensions;
create extension if not exists unaccent with schema extensions;

do $$
begin
  if (
    select n.nspname <> 'extensions'
    from pg_extension e
    join pg_namespace n on n.oid = e.extnamespace
    where e.extname = 'unaccent'
  ) then
    alter extension unaccent set schema extensions;
  end if;
end
$$;

create function venue_name_key(input text) returns text
language sql
stable
set search_path = pg_catalog, extensions
as $$
  select btrim(
    regexp_replace(
      regexp_replace(
        lower(extensions.unaccent(input)),
        '[^[:alnum:][:space:]]+',
        '',
        'g'
      ),
      '\s+', ' ', 'g'
    )
  )
$$;

revoke execute on function venue_name_key(text) from public, anon, authenticated, service_role;
