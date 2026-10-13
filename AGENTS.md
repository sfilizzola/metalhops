# MetalHops agent guide

MetalHops is an Untappd-style app for finding rock and metal bars. Product
vision and roadmap live in [README.md](README.md); read it for context, but
the rules below are what to follow.

## Stacks

| Path | What | Status |
| --- | --- | --- |
| `backend/` | Go REST API (Render) | Active |
| `android/` | Kotlin, Jetpack Compose app | Not started |
| `infra/` | Supabase migrations, Render blueprint | Shared |
| `.github/workflows/` | CI | Shared |

## Current focus

Backend API.

## Scope

- The repository is always opened at the root, but most tasks touch one stack.
- Work in the stack the task names and follow that stack's guide.
- Change shared files (`infra/`, CI, docs) only when the task needs them.
- Ask before editing another stack's code.

## Before working in a stack

Read that folder's `AGENTS.md` first:

- `backend/AGENTS.md`
- `android/AGENTS.md`

## Repo-wide rules

- Branch from `dev` as `feature/<name>`; merge into `dev` by pull request.
  `main` is release-ready only.
- Never commit secrets. `.env` files stay local; `.env.example` is the
  template.
- Follow `.editorconfig`.
- Write commands in project files with plain tools (`make`, `go`, `git`),
  never personal CLI wrappers or aliases; contributors may not have them.
- Keep these agent files current: when a command, path, or rule changes,
  update the matching `AGENTS.md` in the same pull request.
