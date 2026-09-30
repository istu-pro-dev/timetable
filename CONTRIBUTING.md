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

### Branch protection

A repository ruleset protects `develop` and `main`:

- changes land only through a pull request (direct pushes are rejected); approving reviews are not required;
- required status checks: `backend`, `frontend`, `docker` (`.github/workflows/ci.yml`) and `pr-title` (`.github/workflows/pr-title.yml`);
- squash is the only merge method, history must be linear, force pushes and branch deletion are blocked.

Merged branches are deleted automatically. Renaming a CI job means updating the ruleset in the same change.

### Agent / automated flow

```sh
git switch -c feature/<N>-<slug> origin/develop
# ... commit (Conventional Commits) ...
git push -u origin feature/<N>-<slug>
gh pr create --base develop --title "<type>(<scope>): <summary>" --body "... Closes #<N>"
gh pr merge --auto --squash --delete-branch   # merges itself once the required checks pass
```

## Releases

1. `git switch -c release/x.y origin/develop`; only stabilization fixes go there (PRs into `release/x.y`).
2. Open a PR `release/x.y` → `main` and merge it once CI is green.
3. Tag the merge commit on `main`: `git tag vX.Y.Z <sha> && git push origin vX.Y.Z` — CI publishes the
   images with `latest` and the semver tag.
4. Back-merge: open a PR `main` → `develop` (or `release/x.y` → `develop`) so fixes made during
   stabilization reach `develop`.

Hotfixes follow the same path from `hotfix/x.y.z` (branched from `main`): PR into `main`, tag, back-merge into `develop`.

## Planning

Work is tracked on the [project board](https://github.com/orgs/istu-pro-dev/projects/1): weekly sprints (Iteration), milestones per stage, epics as parent issues with sub-issues.
