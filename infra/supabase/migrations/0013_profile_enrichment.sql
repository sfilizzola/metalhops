-- Profile enrichment: optional bio, gender, home place, and avatar, plus the
-- avatars Storage bucket the apps upload to directly.

alter table profiles
  add column bio text,
  add column gender text,
  add column from_city text,
  add column from_country text,
  add column avatar_url text,
  add constraint profiles_bio_length check (
    bio is null or char_length(bio) <= 280
  ),
  add constraint profiles_gender_check check (
    gender is null or gender in ('woman', 'man', 'non_binary', 'prefer_not_to_say')
  ),
  add constraint profiles_from_city_length check (
    from_city is null or char_length(from_city) between 1 and 80
  ),
  add constraint profiles_from_country_format check (
    from_country is null or from_country ~ '^[A-Z]{2}$'
  ),
  add constraint profiles_avatar_url_length check (
    avatar_url is null or char_length(avatar_url) <= 500
  );

-- Hosted Supabase always has the storage schema; the plain Postgres test
-- harness does not, so this part is skipped there.
do $$
begin
  if to_regnamespace('storage') is null then
    return;
  end if;

  insert into storage.buckets (id, name, public, file_size_limit, allowed_mime_types)
  values ('avatars', 'avatars', true, 2097152, array['image/jpeg', 'image/png', 'image/webp'])
  on conflict (id) do nothing;

  -- Each signed-in User manages only objects under avatars/<their user id>/.
  create policy avatars_select_own on storage.objects
    for select to authenticated
    using (bucket_id = 'avatars' and (storage.foldername(name))[1] = auth.uid()::text);
  create policy avatars_insert_own on storage.objects
    for insert to authenticated
    with check (bucket_id = 'avatars' and (storage.foldername(name))[1] = auth.uid()::text);
  create policy avatars_update_own on storage.objects
    for update to authenticated
    using (bucket_id = 'avatars' and (storage.foldername(name))[1] = auth.uid()::text)
    with check (bucket_id = 'avatars' and (storage.foldername(name))[1] = auth.uid()::text);
  create policy avatars_delete_own on storage.objects
    for delete to authenticated
    using (bucket_id = 'avatars' and (storage.foldername(name))[1] = auth.uid()::text);
end
$$;
