# build-workflow Specification

## Purpose
Defines the GitHub Actions build workflow structure: a single `build.yml` for Docker image building/pushing and GitHub Release creation, with `ci.yml` limited to test and lint.

## Requirements
### Requirement: Single build workflow file
The system SHALL have a single workflow file `build.yml` responsible for all Docker image building, pushing, and GitHub Release creation.

#### Scenario: Workflow file exists
- **WHEN** inspecting `.github/workflows/`
- **THEN** `build.yml` exists and contains all Docker build and release logic

### Requirement: Build triggers on all branch pushes and version tags
The workflow SHALL trigger on push events to all branches and on tags matching `v*`.

#### Scenario: Branch push triggers build
- **WHEN** a commit is pushed to any branch
- **THEN** the `build.yml` workflow runs and builds a Docker image

#### Scenario: Version tag triggers build
- **WHEN** a tag matching `v*` is pushed
- **THEN** the `build.yml` workflow runs and builds a Docker image

#### Scenario: Pull request does not trigger build
- **WHEN** a pull request is opened or updated
- **THEN** the `build.yml` workflow does NOT run

### Requirement: GitHub Release created on version tags
The workflow SHALL create a GitHub Release with auto-generated release notes when a version tag is pushed.

#### Scenario: Version tag creates release
- **WHEN** a tag `v1.2.3` is pushed
- **THEN** a GitHub Release is created with auto-generated release notes

#### Scenario: Branch push does not create release
- **WHEN** a commit is pushed to any branch
- **THEN** no GitHub Release is created

### Requirement: CI workflow contains only test and lint
The `ci.yml` workflow SHALL contain only test and lint jobs, with no Docker build logic.

#### Scenario: CI has no Docker job
- **WHEN** inspecting `ci.yml`
- **THEN** it contains `test` and `lint` jobs only, with no Docker-related steps

### Requirement: Release workflow is removed
The `release.yml` workflow file SHALL be deleted.

#### Scenario: No release.yml exists
- **WHEN** inspecting `.github/workflows/`
- **THEN** `release.yml` does not exist
