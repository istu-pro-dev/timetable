# Contributing

## Branching — git flow

| Branch | Purpose |
|---|---|
| `main` | Released code only. Updated from `release/*` and `hotfix/*`. Every merge is tagged `vX.Y.Z`. |
| `develop` | Integration branch (default). All feature work lands here. |
| `feature/<issue>-<slug>` | New work, branched from `develop`, e.g. `feature/12-dsatur`. |
| `release/<version>` | Release stabilization, branched from `develop`, merged into `main` and back into `develop`. |
| `hotfix/<version>` | Urgent fixes, branched from `main`, merged into `main` and `develop`. |

## Commits — Conventional Commits

```
<type>(<scope>): <summary>
```

Types: `feat`, `fix`, `docs`, `test`, `refactor`, `perf`, `ci`, `chore`, `build`.
Scopes (typical): `solver`, `engine`, `api`, `db`, `web`, `auth`, `mcp`, `infra`.
Breaking changes: `feat(api)!: ...` plus a `BREAKING CHANGE:` footer.

## Pull requests

1. One issue per PR; reference it in the description (`Closes #N`).
2. PRs target `develop` (except `release/*` / `hotfix/*` → `main`).
3. The CI pipeline (lint, tests, Docker image build) must pass.
4. PRs are merged with **squash**; the PR title becomes the commit message, so it must follow Conventional Commits.
5. Auto-merge is enabled: once CI is green and review requirements are met, the PR merges itself.

## Planning

Work is tracked on the [project board](https://github.com/orgs/istu-pro-dev/projects/1): weekly sprints (Iteration), milestones per stage, epics as parent issues with sub-issues.
