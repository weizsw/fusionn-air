## Context

The project currently has two workflow files with overlapping Docker build responsibilities:
- `ci.yml`: runs test, lint, and Docker build/push on every push and PR
- `release.yml`: creates GitHub Release and Docker build/push on `v*` tags

The `latest` tag in `release.yml` uses `enable={{is_default_branch}}` which doesn't work on tag pushes (tags aren't associated with a branch context). Docker images push to GHCR in CI but to both GHCR and Docker Hub in releases. The overall setup is duplicative and the tagging behavior is unreliable.

## Goals / Non-Goals

**Goals:**
- Single source of truth for all Docker image building and pushing
- Predictable, correct tagging: branch name for branch pushes, semver + `latest` for version tags
- Push to both GHCR and Docker Hub for all events
- GitHub Release creation on version tags without a separate workflow

**Non-Goals:**
- Changing the Dockerfile or build arguments
- Adding multi-arch support beyond what exists (already linux/amd64 + linux/arm64)
- Adding test/lint gates before Docker build (CI remains separate)
- Container scanning or signing

## Decisions

### 1. Consolidate Docker build into `build.yml`, remove from `ci.yml` and `release.yml`

**Choice**: Single `build.yml` handles all Docker builds and GitHub Releases.

**Alternatives considered**:
- Keep separate `ci.yml` (with Docker) and `release.yml` -- rejected because it duplicates Docker build config and leads to drift
- Single monolithic workflow for everything (test + lint + Docker) -- rejected because test/lint and Docker builds have different concerns and failure modes

**Rationale**: Separation of concerns (CI validates code, build produces artifacts) with no duplication.

### 2. Use `docker/metadata-action` with explicit `latest` logic

**Choice**: Use `type=raw,value=latest` with `enable=${{ startsWith(github.ref, 'refs/tags/v') }}` instead of `enable={{is_default_branch}}`.

**Rationale**: The `is_default_branch` template doesn't work on tag pushes. Explicit ref-checking is reliable and clear.

### 3. VERSION build arg uses ref_name for tags, sha for branches

**Choice**: Conditional VERSION: `github.ref_name` when a tag is pushed (gives `v1.2.3`), `github.sha` for branch pushes.

**Rationale**: Version tags should embed the semver string. Branch builds use the commit SHA since there's no meaningful version.

### 4. No gating Docker build on CI success

**Choice**: `build.yml` runs independently of `ci.yml` -- no `needs` dependency.

**Rationale**: They're separate workflow files so can't share `needs`. The trade-off is that a broken build could get a Docker image pushed, but branch images are ephemeral and version tags should only be created on known-good commits.

## Risks / Trade-offs

- **[Registry clutter from all-branch builds]** → Branch images accumulate in GHCR and Docker Hub. Mitigation: acceptable for now; can add cleanup action later if needed.
- **[Docker Hub secrets required for branch pushes]** → Currently only needed for releases. Mitigation: secrets must be configured in repo settings; builds will fail clearly if missing.
- **[Tag push doesn't verify CI passed]** → A version tag on a broken commit produces a broken `latest` image. Mitigation: only tag commits that have passed CI; could add a status check requirement on tags in the future.
