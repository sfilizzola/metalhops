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
The permanent removal of a User. Their Profile, Reviews, Check-ins, and all
moderation reports they filed end with them; reports targeting their Profile
or Reviews end too, and their Report quota ends. The Profile and Review
moderation decisions remain as Restriction records without that User's
identity, while a standing Commentary restriction ends. A Venue's eligibility
decisions remain as that Venue's history and are not Restriction records. A
Venue Submission they created remains without a Submitter.
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

**Discovery**:
A reading of the Public catalogue narrowed by Discovery text, city, Genre,
Country code, Venue type, or location. It contains only Venues already in the
Public catalogue.
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
a Community Suspension, or to the decision that begins a Commentary
restriction. The note is required only when the reason is other and may be
added to any reason; the affected User sees the explanation on their own
surface, while reporter notes remain private.
_Avoid_: Report note, private note

**Restriction record**:
A kept record of one stored restriction decision: the decision, the reason,
the note, and any expiry. It does not remember the Username or display name.
Replacing a restriction adds only the new restriction's record. Ending one —
a withdrawal of a rename requirement, the clearing of a Community Suspension,
the lift of a Commentary restriction, or an expiry — is its own record and
carries the Restriction explanation of the restriction that ended. The
Platform Moderator supplies no new reason or note. An attempt to end a
restriction that is not standing leaves no record. After User deletion it
remains without that User's identity. A Venue's eligibility decisions are
that Venue's history, not a Restriction record. It is not a Profile and has
no public identity.
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

**Venue type**:
Bar, pub, or club.
_Avoid_: Genre, category

**Discovery text**:
The letters and digits of a Venue name, address, or city, in order. Case,
accents, punctuation, and spaces do not change it, so `Saint-Étienne` and
`Saint Etienne` share one, and a value with no letter or digit, such as
`!!!`, has none.
_Avoid_: Normalized name

**Normalized name**:
The Venue name with case and accents folded, punctuation removed, and spaces
collapsed to one, so `Iron Maiden` and `IronMaiden` do not share one. A
Duplicate Venue Submission shares a Normalized name and lies within 100
metres.
_Avoid_: Discovery text

**Country code**:
The canonical uppercase code for a Venue's country. It is an officially
assigned ISO 3166-1 alpha-2 code, with `XK` as the sole product exception for
Kosovo. The United Kingdom is `GB`.
_Avoid_: Country name, lowercase code, `UK`

**Venue Submission**:
A User's proposal to add a Venue to the catalogue. Only its Submitter can
correct it while pending or rejected. Correcting a rejected one, even with
no field changed, returns it to pending review and to the public catalogue; after User deletion no User can correct it.
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
catalogue. Once that Venue is verified, the Submitter may Review it.
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
a later one. A Platform Moderator's decision on the Venue, including a
correction that changes it, resolves every report still pending for that Venue;
a correction that changes nothing resolves none, and dismissing one report
resolves only that report. A Venue that is rejected, Delisted, or unknown
cannot be reported, and looks the same as one that does not exist.
_Avoid_: Venue edit, Venue Submission

**Genre**:
A rock- or metal-related music category associated with a Venue. A Venue may
have many Genres, and a Discovery may match any selected Genre.
_Avoid_: Music category, tag

**Rejection Feedback**:
The structured reason and optional moderator note explaining why a Venue
Submission was rejected and what the Submitter may need to correct.
_Avoid_: Rejection message

**Review**:
One User's assessment of a Verified Venue. That User has one Review for a
Venue, containing a required whole-number rating from 1 through 5 and optional
written commentary. Five is the strongest assessment and one is the weakest. It
cannot target a Venue that is not verified, and a pending Venue has none. A
rating without commentary is still a complete Review. If a Platform
Moderator removes commentary, that Review remains rating-only, and a reader
can still tell the commentary was removed. Lifting the Commentary restriction
does not bring those words back or hide the removal: the Review stays
rating-only, and the removal stays visible, until the User writes commentary
again. A Review the User saved with no commentary does not show a removal.
The User may end their
Review, and assessing that Venue again is a new Review. The average of a
Venue's ratings belongs to that Venue and is the same for every reader.
_Avoid_: Rating record, comment

**Hidden Commentary**:
Review commentary temporarily removed from public view by a Verified Venue
Owner while a Platform Moderator decides whether to restore or remove it. The
Review's rating remains public and unchanged.
_Avoid_: Deleted Review

**Commentary restriction**:
An indefinite restriction placed by a Platform Moderator on one User writing
Review commentary for one Venue after removing commentary or deleting a Review.
It lasts until a Platform Moderator lifts it through a Review that User
currently has for that Venue; once lifted, the User may add commentary to
that Review or a later one, and a lift after the Review has ended waits
until the User has one there again. The lift leaves a Restriction record on
the Review through which it was lifted, carrying the Restriction explanation
of the decision that began it. The record of that beginning stays on the
Review it was made on, even after that Review has ended. While it stands,
ratings remain allowed and the User sees its Restriction explanation on
their own Review. A further removal or deletion while one stands does not
start another; its start and its Restriction explanation stay the ones that
began it. Once lifted, that restriction does not resume. A later removal or
deletion places a new Commentary restriction, with its own start and its own
Restriction explanation. A lift when none stands leaves no Restriction
record and does not change the Review. Repeating it still leaves no record.
It can be lifted on a verified Venue or a Delisted Venue. A pending or
rejected Venue has no Review, so there is nothing to lift through.
_Avoid_: Commentary lock, permanent ban

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
one. A rating-only Review, including one whose commentary a Platform
Moderator removed, cannot be reported. Lifting a Commentary restriction is
not an assessment. Removing commentary already resolved any report that was
waiting, and ending the Review ends its reports, so a lift has no pending
Review Report. Ending the Review does not return their Report quota slots.
_Avoid_: Review moderation

**Resolved Review Report**:
A Review Report no longer waiting for assessment because a Platform Moderator
acted or dismissed it, its Venue was Delisted, or its author changed or cleared
the reported commentary. Lifting a Commentary restriction is not an
assessment and does not resolve one. An author-resolved report is not a
moderator dismissal.
_Avoid_: Deleted report

**Check-in**:
A User's memory of visiting a Venue in the public catalogue at one fixed
stated time, recorded during the visit or within 24 hours. The same User may
have many Check-ins at that Venue; correcting one removes it and records
another, and each remains in that User's history if the Venue is later
rejected, Delisted, or returns to the public catalogue.
_Avoid_: Verified presence, location proof, visit log, Visit

**Check-in history**:
That User's own Check-ins. The User can read it while a Community Suspension
or a rename requirement stands. Recording a Check-in and removing one are
contribution, so both are refused while either stands. A sign-in that is not
yet a User has none.
_Avoid_: Activity feed, public history, Check-in feed

**Check-in feed**:
A public Venue's list of its most recent 20 Check-ins, newest first, each
showing its Checker's Public identity. Any User may view it, including one
who must rename or is suspended; a sign-in that is not yet a User cannot.
There is no per-Check-in private toggle. The `check_in_count` number is
unrelated and stays visible to a sign-in that is not yet a User.
_Avoid_: Activity feed, who's here, Check-in history

**Check-in spacing**:
The one-hour gap required between one User's remaining Check-ins at one
Venue, including after that Venue is rejected or Delisted and then returns
to the public catalogue. A gap of one hour or smaller is refused, any later
stated time is allowed, including one microsecond past the hour, and removing
a Check-in removes its spacing; another User's Check-in has none.
_Avoid_: Cooldown, rate limit, global spacing

## Flagged ambiguities

**Anonymous Venue viewing**: Whether a sign-in that is not yet a User sees
only a reduced "basics" view of a Venue, versus full Venue details as today,
is undecided and deferred to a future redesign. Until resolved, Session 13
changes nothing about existing open venue detail/review routes; it adds only
the Check-in feed, which already requires being a User.

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

**Developer:** This Venue is still pending, and it is on the public catalogue. Are there Reviews on it?

**Domain expert:** No. A pending Venue has none. Reviews open once it is verified.

**Developer:** The User saves the same rating and the same commentary again. Has the Review changed?

**Domain expert:** No. It is the same assessment.

**Developer:** The Submitter rates a Venue after it is verified. Is that Review allowed?

**Domain expert:** Yes. Submitting a Venue does not keep its Submitter from reviewing it.

**Developer:** Two saves for the same User and Venue arrive together, with different ratings. How many Reviews remain?

**Domain expert:** One. The assessment that lands last is the Review.

**Developer:** Is a rating of four and a half a Review?

**Domain expert:** No. The rating is a whole number from 1 through 5. Five is the strongest assessment and one is the weakest.

**Developer:** The User removes their Review, then rates the Venue again. Is it the same Review?

**Domain expert:** No. The first one has ended. The later assessment is a new Review.

**Developer:** This User rated the Venue 5, and the public average is 4.2. Does that User see a different average?

**Domain expert:** No. The average belongs to the Venue and is the same for every reader. That User's Review is their own.

**Developer:** Can a Verified Venue Owner delete a negative Review?

**Domain expert:** No. They may temporarily hide its commentary for review,
but the rating stays public and a Platform Moderator makes the final decision.

**Developer:** A Platform Moderator deleted abusive commentary, and the User
later created a new Review for that Venue. Can they add commentary?

**Domain expert:** Not while the commentary restriction stands. They may still
rate the Venue. After a Platform Moderator lifts it, they may add commentary
to that Review or a later one.

**Developer:** A Platform Moderator deleted the Review that began a Commentary
restriction, and the User has not rated that Venue again. Can the moderator
lift it?

**Domain expert:** No. The restriction remains, and the lift needs a Review
that User currently has for that Venue. It waits until they rate it again.

**Developer:** The moderator removed "The pit is loud," then lifted the
Commentary restriction. The User has not saved since. What does the Review
contain?

**Domain expert:** It stays rating-only. Those words do not come back, and a
reader can still tell the commentary was removed. The User may write
commentary again, and that save is new commentary. A save with no commentary
leaves it rating-only and still shows the removal. A Review that never had
commentary does not show one.

**Developer:** A Platform Moderator removed commentary for harassment, with
the note "threats in the second sentence," then lifted the Commentary
restriction and supplied no reason. What does the lift's record remember?

**Domain expert:** Harassment, and that note. The record is the fact that
the restriction ended, carrying the Restriction explanation of the decision
that began it. Ending a rename requirement or a Community Suspension is
recorded the same way.

**Developer:** Review A was deleted for harassment. The User then created
rating-only Review B, and the Platform Moderator lifted the Commentary
restriction through Review B. Which Review does the lift's record belong to?

**Domain expert:** Review B. Review A's record stays the decision that began
the restriction, even though Review A has ended. Both carry the same
Restriction explanation. There is still one Commentary restriction for that
User and Venue.

**Developer:** The Venue was Delisted after the Review was written. Can the
Platform Moderator lift the Commentary restriction? What about a pending
Venue?

**Domain expert:** Yes on the Delisted Venue. The Review is still there. A
pending Venue has no Review, and a rejected Venue has none either. There is
nothing to lift through.

**Developer:** The Platform Moderator lifted the Commentary restriction. The
User writes commentary, and the moderator removes it for spam. Does the
earlier lift end this restriction?

**Domain expert:** No. The earlier lift ended the earlier restriction, and
that one does not resume. This is a new Commentary restriction, with its own
start and its own Restriction explanation.

**Developer:** That new restriction still stands, and the moderator removes
commentary again. Does that start another?

**Domain expert:** No. One is already standing. Its start and its Restriction
explanation stay the ones that began it.

**Developer:** A Review Report was pending. The Platform Moderator removed
the commentary, then lifted the Commentary restriction. Did the lift assess
the report?

**Domain expert:** No. Removing the commentary already resolved it. The lift
ends the Commentary restriction and is not an assessment. A rating-only
Review cannot be reported, so the lift has no pending Review Report.

**Developer:** The Platform Moderator lifts through a Review, and that User
and Venue have no Commentary restriction. Does that leave a record?

**Domain expert:** No. Nothing ended, so there is no Restriction record, and
the Review stays as it is. Doing it again still leaves no record. Ending a
rename requirement or a Community Suspension when the Profile is already
clear works the same way.

**Developer:** A Review Report is pending, and the author replaces the reported
commentary before a Platform Moderator assesses it. Is the report still pending?

**Domain expert:** No. It was resolved by the author changing the commentary.
The replacement is new content and may be reported separately.

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

**Developer:** This User already has a Check-in at The Riff from last night,
and they were there again this evening. Is that the same Check-in?

**Domain expert:** No. Each stated time is its own Check-in. Both stay in
that User's history. There is no separate Visit.

**Developer:** The User checked in at The Riff for 8pm, then realized they
were at The Pit. Does that Check-in move to The Pit?

**Domain expert:** No. That Check-in is removed. The Pit is a new Check-in.

**Developer:** User A and User B both check in at The Pit for 10pm. User A
then tries another Check-in there for a stated time closer than the Check-in
spacing. Which ones exist?

**Domain expert:** Both 10pm Check-ins exist. User A's second one does not.
Spacing applies to one User's stated times at one Venue, not to other Users,
and not to the moment the Check-in was recorded.

**Developer:** User A already has a Check-in at The Pit. They try another at
a stated time exactly the Check-in spacing later, and another one second
after that.

**Domain expert:** The equal gap is refused. One second past the spacing is
allowed. A smaller gap is refused too.

**Developer:** The stated time is one microsecond past the Check-in spacing.
Is that Check-in allowed?

**Domain expert:** Yes. Any stated time past the spacing is allowed. The
stated time is not rounded to the nearest second.

**Developer:** User A checks in at The Pit for 10:00. Can they check in again
for 11:00, or for one microsecond after 11:00?

**Domain expert:** 11:00 is refused. One microsecond after 11:00 is allowed.
The Check-in spacing is one hour.

**Developer:** User A checks in at The Pit for 10:00, removes that Check-in,
and records one for 10:30. Is the 10:30 Check-in allowed?

**Domain expert:** Yes. The removed Check-in no longer holds its hour. User
B's Check-ins at The Pit are unchanged.

**Developer:** User A checks in at The Pit for 10:00. The Pit is Delisted,
then verified again at 10:30. User A records a Check-in for 10:40. Does the
10:00 Check-in still exist, and is 10:40 allowed?

**Domain expert:** The 10:00 Check-in stays in User A's history. 10:40 is
refused, because that hour still holds. A Venue can close or leave the
public catalogue, and that history stays. There is no deleted Venue.

**Developer:** User A is under a Community Suspension and opens their
Check-in history. Can they see the 10:00 Check-in at The Pit, and can they
remove it?

**Domain expert:** They can see it. They cannot remove it, and they cannot
record another. A rename requirement is the same split. A sign-in that is
not yet a User has no Check-in history.

**Developer:** A stranger who has not signed in opens The Pit. Do they see
the `check_in_count` number? Do they see who checked in?

**Domain expert:** They see the number. They see nothing about who checked
in; the Check-in feed needs a User. Once they become a User, they see the
most recent 20 Check-ins there, newest first, by Public identity.

**Developer:** User A is suspended and opens The Pit's Check-in feed. Can
they see it?

**Domain expert:** Yes. Viewing the feed is a read, like their own Check-in
history. The suspension stops them from recording or removing a Check-in,
not from reading.

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

**Developer:** That User also had a Commentary restriction, and a Platform
Moderator had verified a Venue. The User deletes their account. What remains?

**Domain expert:** The standing Commentary restriction ends. The Restriction
records of the Profile and of the Reviews remain, without that User's
identity. The Venue's eligibility decisions remain as that Venue's history.
They are not Restriction records.

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

**Developer:** The city is stored as `Saint-Étienne`. Does a city of
`Saint Etienne` match it?

**Domain expert:** Yes. They share one Discovery text. Case, accents,
punctuation, and spaces do not change it.

**Developer:** Someone searches for `IronMaiden`. Is the Venue named
`Iron Maiden` a match, and is a submission of `IronMaiden` a duplicate of it?

**Domain expert:** The search matches, because they share Discovery text.
They are not duplicates. Their Normalized names differ, because the space
still counts.

**Developer:** Does a city filter of `York` include a Venue in `New York`?
Does the query `york`?

**Domain expert:** The city filter does not, because the Discovery text must
be the same. The query does, because `york` occurs inside the city's
Discovery text. A query matches the name, the address, or the city.
