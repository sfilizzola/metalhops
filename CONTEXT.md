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
The permanent removal of a User. Their Profile, Reviews, and Check-ins end
with them. A Venue Submission they created remains, but it no longer has a
Submitter.
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
impersonation, personal information, or spam. It does not automatically alter
that Profile, a change to the Profile leaves a pending report open, and a
resolved report does not stop a later one.
_Avoid_: User Report

**Community Suspension**:
A restriction that prevents a User from contributing while leaving public
discovery, that User's own Check-in history, Venue Submissions, and Reviews
available. It may expire at a set time or remain until a Platform Moderator
clears it.
_Avoid_: Account deletion, authentication ban

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

**Venue**:
A publicly accessible bar, pub, or club where rock or metal is a recurring
part of the normal experience, not merely an occasional event.
_Avoid_: Place, location, event venue

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
A trusted MetalHops operator who decides Venue eligibility and makes final
Review moderation decisions. On every privileged moderation view, that
moderator sees each involved User's full Profile, without email or raw auth
metadata.
_Avoid_: Venue owner

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
