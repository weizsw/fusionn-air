## Why

The current GitHub Actions workflow has a confusing Docker image tagging strategy: `latest` is tied to version tags but doesn't actually work due to `is_default_branch` not being set on tag pushes, Docker build logic is duplicated across `ci.yml` and `release.yml`, and CI triggers Docker builds on every push including throwaway branches mixed in with test/lint jobs.

## What Changes

- **BREAKING**: Remove Docker build job from `ci.yml` -- it becomes test+lint only
- **BREAKING**: Remove `release.yml` entirely -- its responsibilities move to a new `build.yml`
- Create new `build.yml` workflow that consolidates all Docker image building, pushing, and GitHub Release creation
- New tagging strategy:
  - Branch pushes produce a branch-name tag (e.g., `main`, `dev`, `feat-xyz`)
  - Version tag pushes (`v*`) produce semver tags (`1.2.3`, `1.2`, `1`) plus `latest`
  - `latest` is only updated by version tags, not branch pushes
- Push Docker images to both GHCR and Docker Hub for all events (branches and tags)
- GitHub Release creation is embedded in `build.yml`, triggered only on version tags

## Capabilities

### New Capabilities

- `docker-image-tagging`: Unified Docker image tagging strategy covering branch tags, semver tags, and `latest` across both GHCR and Docker Hub
- `build-workflow`: Single `build.yml` workflow for Docker build/push and GitHub Release creation

### Modified Capabilities

_None -- no existing specs are affected._

## Impact

- `.github/workflows/ci.yml`: Remove the `docker` job entirely
- `.github/workflows/release.yml`: Delete this file
- `.github/workflows/build.yml`: New file replacing Docker logic from both workflows
- Docker Hub secrets (`DOCKERHUB_USERNAME`, `DOCKERHUB_TOKEN`) must be configured in GitHub repo settings for all pushes (currently only needed for releases)
