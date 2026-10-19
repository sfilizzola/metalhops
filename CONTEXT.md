# MetalHops

MetalHops helps people discover places whose normal identity is rooted in the
rock and metal scene.

## Language

**User**:
A person with a MetalHops identity and a Profile. Creating a User requires a
Username and a display name. A sign-in that has not supplied both is not a
User, but that unfinished sign-in can still discover public Venues or be
removed. It cannot contribute.
_Avoid_: Account, member

**User id**:
The system's reference for one User. People do not use it to identify that
User. A public Review does not carry it.
_Avoid_: Username, display name

**User deletion**:
The permanent removal of a User. Their Profile, Reviews, Check-ins, and
Profile Reports end with them, whether they filed the report or were its
target. Their report quota ends with them. A restriction record remains,
without that User's identity. A Venue Submission they created remains, but it
no longer has a Submitter.
_Avoid_: Account deletion, Profile deletion

**Profile**:
A User's public community identity, consisting of a unique Username and a
non-unique display name. Creating a User creates the Profile, and people
identify it by its Username. A Profile is required before the User can submit,
Review, or Check in.
_Avoid_: Account, User id

**Public catalogue**:
The same Venues everyone can discover, whether or not the person is a User.
It contains Verified Venues and pending Venue Submissions whose required
fields are valid, with pending Venues visibly not verified, and it excludes
rejected and Delisted Venues.
_Avoid_: Personal catalogue

**Profile Report**:
A User's request for a Platform Moderator to assess another Profile for abuse,
impersonation, personal information, or spam. The reporter's own Profile is
active, and a User cannot report their own Profile. The target may already be
under a rename requirement or a Community Suspension, and the report does not
alter that Profile or lift either restriction. The reporter must explain
impersonation, personal information, and other. That explanation is visible
to a Platform Moderator and not to the reported User.
_Avoid_: User Report

**Pending Profile Report**:
A Profile Report still waiting for assessment. One User has at most one
pending report for a given Profile; another User may have their own. It stays
pending when the Profile changes, a restriction is withdrawn or expires, or a
Platform Moderator repeats the same reason, note, and expiry. One filed after
an assessment is stored is a new pending report.
_Avoid_: Open ticket

**Resolved Profile Report**:
A Profile Report closed by assessment or dismissal. Placing a restriction,
replacing one, or changing its reason, note, or expiry resolves every report
still pending for that Profile. Dismissing one report resolves only that
report and leaves the Profile unchanged. Dismissal does not change a report
a restriction already resolved, and dismissing an already dismissed report
leaves it dismissed. A resolved report does not stop a later one.
_Avoid_: Deleted report

**Report quota**:
A User's limit of 20 moderation reports in a rolling 24 hours. It covers
Profile Reports, Venue Reports, and Review Reports. Filing a report uses one
slot, and that slot stays used if the report is later dismissed, resolved, or
ended by the target's User deletion. A report that is never filed uses no
slot.
_Avoid_: Rate limit, per-type quota

**Community Suspension**:
A restriction that prevents a User from contributing while leaving public
discovery, that User's own Check-in history, Venue Submissions, and Reviews
available. While it stands, the User cannot replace the Username or display
name. It may expire, a Platform Moderator may clear it, or a rename
requirement placed on that Profile ends it early. Once ended, it does not
return.
_Avoid_: Account deletion, authentication ban, Rename requirement

**Rename requirement**:
A Platform Moderator's objection to a Profile's public identity, with no
expiry. The User may change the Username or display name while it stands, but
that edit does not end it. It ends when a Platform Moderator withdraws it or
places a Community Suspension on that Profile. Once ended, it does not return.
_Avoid_: Community Suspension, ban

**Restriction explanation**:
The reason and note a Platform Moderator attaches to a rename requirement or
a Community Suspension. The note is required only when the reason is other,
and it may be added to any reason. The User sees it on their own Profile. A
Community Suspension also shows its expiry there when one is set.
_Avoid_: Report note, private note

**Restriction record**:
A kept record of one stored restriction decision: the decision, the reason,
the note, and any expiry. It does not remember the Username or display name.
Replacing a restriction adds only the new restriction's record, and a
withdrawal or expiry is its own record. After User deletion it remains
without that User's identity. It is not a Profile and has no public identity.
_Avoid_: Profile, public identity

**Username**:
The unique public handle by which people identify a User. The User may choose
its letter casing and later correct that casing without creating a different
handle; a different spelling, including an underscore, replaces the previous
Username immediately and keeps no history.
_Avoid_: Display name, User id

**Protected Username**:
A Username reserved for the platform when its disguise matches a protected
name exactly, or when it contains MetalHops. Underscores and lookalike digits
do not make it available, and a display name is never protected.
_Avoid_: Display name

**Display name**:
The non-unique name a User chooses to show. It may match another display name,
another Username, or a protected word, and it does not identify the User.
_Avoid_: Username, User id

**Public identity**:
The name a public surface shows for a Profile. While a rename requirement or
a Community Suspension stands, it is the display name MetalHops User and no
Username, even if the stored name changed. A pending Profile Report does not
change it. The User's own Profile and a Platform Moderator see the stored
Username and display name, and a public surface shows that stored identity
again once the restriction ends.
_Avoid_: User id

**Venue**:
A publicly accessible bar, pub, or club where rock or metal is a recurring
part of the normal experience, not merely an occasional event.
_Avoid_: Place, location, event venue

**Country code**:
The canonical uppercase code for a Venue's country. It is an officially
assigned ISO 3166-1 alpha-2 code, with `XK` as the sole product exception for
Kosovo. The United Kingdom is `GB`.
_Avoid_: Country name, lowercase code, `UK`

**Venue Submission**:
A User's proposal to add a Venue to the catalogue. Only its Submitter can
correct it while pending or rejected, which returns it to pending review and
to the public catalogue; after User deletion no User can correct it.
_Avoid_: Venue claim, listing

**Duplicate Venue Submission**:
A Venue Submission that refers to the same real-world Venue as an existing
Venue or another active Venue Submission.
_Avoid_: Similar Venue

**Submitter**:
The User who created a Venue Submission, not its owner, and not shown in the
public catalogue. User deletion leaves the Venue Submission with no Submitter,
nobody else becomes one, and any User may report that public Venue. The User
recognizes their own Venue from their submission list, not from the public
catalogue.
_Avoid_: Owner

**Verified Venue Owner**:
A future User whose authority to represent a Venue has been confirmed. A
Verified Venue Owner is distinct from a Submitter and from a Verified Venue.
_Avoid_: Submitter, Venue admin

**Verified Venue**:
A Venue Submission accepted as eligible for the public catalogue. Verification
does not mean that a Venue owner submitted or claimed it.
_Avoid_: Approved place

**Delisted Venue**:
A formerly Verified Venue removed from the public catalogue because it closed,
became a duplicate, or no longer qualifies. Reopening restores that same Venue
and its past Check-ins and Reviews; while Delisted, a User cannot discover it
or start a new Check-in, and pending Review Reports from the delisting stay
resolved.
_Avoid_: Rejected Venue, deleted Venue

**Venue Report**:
A User's request for a Platform Moderator to assess potentially incorrect or
outdated information about a public Venue that the reporter did not submit.
The Submitter is not offered a report on their own Venue, a person who is not
yet a User is not offered one either. A report does not automatically hide or
alter that Venue, and a resolved report stays resolved if the Venue returns
to the public catalogue. A resolved report does not stop that User from filing
a later one.
_Avoid_: Venue edit, Venue Submission

**Genre**:
A rock- or metal-related music category associated with a Venue. A Venue may
have many Genres, and discovery may match any selected Genre.
_Avoid_: Music category, tag

**Rejection Feedback**:
The structured reason and optional moderator note explaining why a Venue
Submission was rejected and what the Submitter may need to correct.
_Avoid_: Rejection message

**Review**:
A User's assessment of one Verified Venue, containing a required rating and
optional written commentary. It cannot target a Venue that is not verified. A
rating without commentary is still a complete Review; if a Platform Moderator
removes commentary, that Review remains rating-only.
_Avoid_: Rating record, comment

**Hidden Commentary**:
Review commentary temporarily removed from public view by a Verified Venue
Owner while a Platform Moderator decides whether to restore or remove it. The
Review's rating remains public and unchanged.
_Avoid_: Deleted Review

**Platform Moderator**:
A trusted MetalHops operator who decides Venue eligibility, Profile
restrictions, and final Review moderation. A Platform Moderator is not a User
and has no Profile. On every privileged moderation view, that moderator sees
each involved User's full Profile, without email or raw auth metadata.
_Avoid_: Venue owner, management

**Review Report**:
A User's request for a Platform Moderator to assess another User's public
Review commentary for a specific safety reason. It does not hide or alter the
Review, and a resolved report does not stop the same User from filing a later
one.
_Avoid_: Review moderation

**Check-in**:
A User's memory of visiting a Verified Venue or a pending Venue in the public
catalogue at a stated time. It remains in that User's history if the Venue is
later rejected, closes, or returns to the public catalogue, and it may be
recorded during the visit or retrospectively within 24 hours.
_Avoid_: Verified presence, location proof, visit log

## Example dialogue

**Developer:** Should we list a concert hall that hosts two metal shows each
month but has no rock or metal identity outside those events?

**Domain expert:** No. It hosts relevant events, but it is not a MetalHops
Venue. A qualifying Venue has a recurring rock or metal identity.

**Developer:** Can the Submitter fix incorrect coordinates after rejection?

**Domain expert:** Yes. Correcting the Venue Submission sends it back for
review, but the Submitter is not necessarily the Venue's owner.

**Developer:** Is a four-star rating without text a Review?

**Domain expert:** Yes. Commentary is optional, but every Review has a rating.

**Developer:** Can a Verified Venue Owner delete a negative Review?

**Domain expert:** No. They may temporarily hide its commentary for review,
but the rating stays public and a Platform Moderator makes the final decision.

**Developer:** Should the app show people the User id?

**Domain expert:** No. People identify a User by Username. The User id is only
the system's reference.

**Developer:** If someone confirms removal of their MetalHops identity, do they
delete only their Profile?

**Domain expert:** No. That is User deletion. The Profile ends with the User.
Their Venue Submission remains, but it has no Submitter.

**Developer:** Can someone add yesterday evening's visit this morning?

**Domain expert:** Yes, if it was within the last 24 hours. The Check-in is a
user declaration, not proof of physical presence.

**Developer:** The Username is `HateFan` and a rename requirement is in place.
The User changes it to `MetalFan`. Are they free to contribute?

**Domain expert:** No. The Profile now shows `MetalFan`, but the rename
requirement remains. Only a Platform Moderator can withdraw it.

**Developer:** `MetalFan` is still under that rename requirement. Can someone
report that Username?

**Domain expert:** Yes. The Profile Report asks a Platform Moderator to assess
the Profile. It does not lift the rename requirement.

**Developer:** That report is still pending, and the Platform Moderator
withdraws the rename requirement. Is the report resolved?

**Domain expert:** No. Withdrawing the objection does not assess the report.
It stays pending.

**Developer:** `MetalFan` is under a rename requirement and a Profile Report is
pending. The Platform Moderator places a Community Suspension. What remains?

**Domain expert:** The rename requirement ends. The Profile is under the
Community Suspension, and that pending report is resolved. A report filed
afterward is a new request.

**Developer:** `MetalFan` is under a Community Suspension with time still left,
and a Profile Report is pending. The Platform Moderator places a rename
requirement. What remains?

**Domain expert:** The suspension ends and does not return. The Profile is
under the rename requirement, and that pending report is resolved.

**Developer:** `MetalFan` is suspended until 1 November for spam, and a Profile
Report is pending. The Platform Moderator moves the expiry to 1 December. Is
the report still pending?

**Domain expert:** No. A different expiry is a new assessment, so the report
is resolved. The same reason, note, and expiry again would leave it pending.

**Developer:** A rename requirement stands, and the User has changed the stored
Username to `MetalFan`. What does a public surface show?

**Domain expert:** MetalHops User, and no Username. The User and a Platform
Moderator still see `MetalFan`.

**Developer:** The Platform Moderator withdraws that rename requirement while
a Profile Report about `MetalFan` is still pending. What does a public surface
show?

**Domain expert:** `MetalFan`. A pending Profile Report does not hide the
Profile. The report stays pending.

**Developer:** The Platform Moderator's note says "Use a personal name." The
reporter's note says "This matches a local promoter." What does the User see
on their own Profile?

**Domain expert:** The restriction explanation. The reporter's explanation
stays on the Profile Report.

**Developer:** Can a Platform Moderator place a rename requirement for
impersonation without a note?

**Domain expert:** Yes. A note is required only when the reason is other. The
reporter must still explain impersonation, personal information, and other.

**Developer:** Someone reports `HateFan`, and then `HateFan` deletes their
account. The reporter is at 20 for the day and files again. Does the earlier
report still use a slot?

**Domain expert:** Yes. Filing used the slot. The target's User deletion
removes the Profile Report and does not return it.

**Developer:** A User reports `MetalFan` for spam, then tries again for
impersonation before anyone acts. Is the second report filed?

**Domain expert:** No. That User already has a pending Profile Report for
`MetalFan`. Another User may still report that Profile.

**Developer:** A Platform Moderator suspends `HateFan`, and `HateFan` deletes
their account. Does the record of that suspension remain?

**Domain expert:** Yes. It remains without that User's identity. It is not a
Profile and not a public surface.

**Developer:** A rename requirement is already recorded, and a Platform
Moderator replaces it with a Community Suspension. How many new records are
added?

**Domain expert:** One, for the suspension. The earlier rename record stays.
A withdrawal or an expiry is its own record.

**Developer:** The rename requirement was recorded while the Username was
`HateFan`. The User then changes it to `MetalFan`. Does the record remember
`HateFan`?

**Domain expert:** No. It remembers the decision, the reason, the note, and
any expiry. It does not remember the Username or display name.

**Developer:** Is the report quota 20 Profile Reports, or 20 moderation
reports of any kind?

**Domain expert:** Any kind. Profile Reports, Venue Reports, and Review
Reports share the same 20 in a rolling 24 hours.

**Developer:** A Profile Report was resolved because a Community Suspension
was placed. The Platform Moderator dismisses that same report. Does the
report change?

**Domain expert:** No. It stays resolved by the suspension. Dismissal
resolves a pending report only.
