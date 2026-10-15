-- Profile identity. Venue submissions stay when the submitter is deleted.

create table profiles (
  id uuid primary key references auth.users (id) on delete cascade,
  username text not null,
  display_name text not null,
  status text not null default 'active',
  moderation_reason text,
  moderation_note text,
  suspended_until timestamptz,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  constraint profiles_username_format check (
    username ~ '^[A-Za-z0-9_]{3,30}$'
  ),
  constraint profiles_display_name_length check (
    char_length(display_name) between 1 and 80
  ),
  constraint profiles_status_check check (
    status in ('active', 'rename_required', 'suspended')
  ),
  constraint profiles_moderation_note_length check (
    moderation_note is null or char_length(moderation_note) <= 1000
  ),
  constraint profiles_active_fields_check check (
    status <> 'active'
    or (
      moderation_reason is null
      and moderation_note is null
      and suspended_until is null
    )
  )
);

create unique index profiles_username_identity_key on profiles (lower(username));

alter table profiles enable row level security;
revoke all on table profiles from public, anon, authenticated, service_role;

alter table venues drop constraint venues_submitted_by_fkey;
alter table venues
  add constraint venues_submitted_by_fkey
  foreign key (submitted_by) references auth.users (id) on delete set null;
