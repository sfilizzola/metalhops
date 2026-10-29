# Profile roadmap

What the Profile shows today, and what is planned. The profile page already
reserves a "Coming soon" slot for each planned item.

## Shipped in the enriched Profile

- Picture, short bio, From (city and country), gender (optional), and the email
  used to sign up.
- Read-only counters: total check-ins and unique venues checked into.

## Planned: following and followers

Users follow other users. "Friends" in the product copy means following and
followers, not a mutual-approval relationship, until a privacy decision says
otherwise.

- Data: `follows(follower_id, followee_id, created_at)`, primary key on both
  columns, check `follower_id <> followee_id`, index on `followee_id`.
- API: the profile `stats` object gains `following` and `followers` counts.
  Follow and unfollow are new app routes.
- Open question: are profiles public to follow, or do follows need approval?

## Planned: crews

A user can belong to several crews, and a crew has several users
(many-to-many).

- Data: `crews(id, name, created_at, ...)` and
  `crew_members(crew_id, user_id, role, joined_at)`, primary key
  `(crew_id, user_id)`.
- API: the profile gains `crews: [{id, name}]`. The page shows "no crew" while
  the list is empty.
- Open questions: who can create a crew, how members join, and crew roles.

## Planned: moderation of the new profile content

Moderation reasons today are name-focused (`abusive_name`, `impersonation`,
and so on). Bio and picture are new user-written content and need their own
reasons, report flow, and operator actions. Until then an operator can suspend
the whole profile.

## Planned: public profile view

Viewing another user's profile, with privacy rules for gender and From.
Today only the owner sees these fields.
