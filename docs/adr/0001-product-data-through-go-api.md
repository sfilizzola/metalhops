# Route all product data through the Go API

MetalHops clients authenticate directly with Supabase Auth, but Android, later
mobile clients, and the future admin frontend read and write product data only
through the Go API. Although Supabase can expose tables directly under RLS, a
single API contract keeps visibility, moderation, validation, and audit rules
consistent across clients. RLS denies anonymous product access and direct
product writes as defense in depth; authenticated author-only private reads
may remain.
Operator SQL runbooks are a temporary moderation bridge and must be retired as
privileged Go API workflows become available.
