-- Commentary restriction lift: widen the review moderation audit action check.
alter table review_moderation_actions
  drop constraint review_moderation_actions_action_check;
alter table review_moderation_actions
  add constraint review_moderation_actions_action_check check (
    action in ('commentary_removed', 'review_deleted', 'commentary_restriction_lifted')
  );
