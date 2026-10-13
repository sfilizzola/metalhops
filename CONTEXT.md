# MetalHops

MetalHops helps people discover places whose normal identity is rooted in the
rock and metal scene.

## Language

**User**:
A person with a MetalHops identity who may discover Venues and, after creating
a Profile, contribute to the community.
_Avoid_: Account, member

**Profile**:
A User's public community identity, consisting of a unique Username,
non-unique display name. A Profile is required before the User can submit,
Review, or Check in.
_Avoid_: Account

**Profile Report**:
A User's request for a Platform Moderator to assess another Profile for abuse,
impersonation, personal information, or spam. It does not automatically alter
that Profile.
_Avoid_: User Report

**Community Suspension**:
A restriction that prevents a User from contributing while leaving public
discovery available. It may expire at a set time or remain until a Platform
Moderator clears it.
_Avoid_: Account deletion, authentication ban

**Username**:
A User's unique, editable public handle. It is distinct from the non-unique
display name.
_Avoid_: Display name

**Venue**:
A publicly accessible bar, pub, or club where rock or metal is a recurring
part of the normal experience, not merely an occasional event.
_Avoid_: Place, location, event venue

**Venue Submission**:
A User's proposal to add a Venue to the catalogue. A pending or rejected
Venue Submission can be corrected; a correction returns it to pending review.
_Avoid_: Venue claim, listing

**Duplicate Venue Submission**:
A Venue Submission that refers to the same real-world Venue as an existing
Venue or another active Venue Submission.
_Avoid_: Similar Venue

**Submitter**:
The User who created a Venue Submission. This does not imply that the User
owns or represents the Venue.
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
A formerly Verified Venue removed from public discovery because it closed,
became a duplicate, or no longer qualifies. Its history is retained.
_Avoid_: Rejected Venue, deleted Venue

**Venue Report**:
A User's request for a Platform Moderator to assess potentially incorrect or
outdated information about a Verified Venue. A Venue Report does not
automatically hide or alter the Venue.
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
A User's assessment of one Venue, containing a required rating and optional
written commentary. A rating without commentary is still a complete Review;
if a Platform Moderator removes commentary, that Review remains rating-only.
_Avoid_: Rating record, comment

**Hidden Commentary**:
Review commentary temporarily removed from public view by a Verified Venue
Owner while a Platform Moderator decides whether to restore or remove it. The
Review's rating remains public and unchanged.
_Avoid_: Deleted Review

**Platform Moderator**:
A trusted MetalHops operator who decides Venue eligibility and makes final
Review moderation decisions.
_Avoid_: Venue owner

**Review Report**:
A User's request for a Platform Moderator to assess Review commentary for a
specific safety reason. A Review Report does not hide or alter the Review.
_Avoid_: Review moderation

**Check-in**:
A User's declaration that they visited a Venue at a stated time. It may be
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

**Developer:** Can someone add yesterday evening's visit this morning?

**Domain expert:** Yes, if it was within the last 24 hours. The Check-in is a
user declaration, not proof of physical presence.
