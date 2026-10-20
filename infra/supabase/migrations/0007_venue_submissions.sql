-- Venue submissions: quota count and the Submitter's list.
create index venues_submitted_by_created_at_idx on venues (submitted_by, created_at);
create index venues_submitted_by_updated_at_idx on venues (submitted_by, updated_at desc, id desc);
