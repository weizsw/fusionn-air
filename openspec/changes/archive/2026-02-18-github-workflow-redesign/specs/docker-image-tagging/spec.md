## ADDED Requirements

### Requirement: Branch push produces branch-name tag
The system SHALL tag Docker images with the branch name when a push event occurs on any branch.

#### Scenario: Push to main branch
- **WHEN** a commit is pushed to the `main` branch
- **THEN** the Docker image is tagged `main` and pushed to both GHCR and Docker Hub

#### Scenario: Push to dev branch
- **WHEN** a commit is pushed to the `dev` branch
- **THEN** the Docker image is tagged `dev` and pushed to both GHCR and Docker Hub

#### Scenario: Push to feature branch
- **WHEN** a commit is pushed to a branch named `feat-xyz`
- **THEN** the Docker image is tagged `feat-xyz` and pushed to both GHCR and Docker Hub

### Requirement: Version tag produces semver tags and latest
The system SHALL tag Docker images with semver variants and `latest` when a version tag is pushed.

#### Scenario: Push version tag v1.2.3
- **WHEN** a git tag `v1.2.3` is pushed
- **THEN** the Docker image is tagged `1.2.3`, `1.2`, `1`, and `latest`
- **THEN** all four tags are pushed to both GHCR and Docker Hub

#### Scenario: Push version tag v2.0.0
- **WHEN** a git tag `v2.0.0` is pushed
- **THEN** the Docker image is tagged `2.0.0`, `2.0`, `2`, and `latest`

### Requirement: Branch push does not produce latest tag
The system SHALL NOT tag Docker images with `latest` on branch pushes.

#### Scenario: Push to main does not set latest
- **WHEN** a commit is pushed to the `main` branch
- **THEN** the Docker image is tagged `main` only, NOT `latest`

### Requirement: Images pushed to both registries
The system SHALL push Docker images to both GHCR (`ghcr.io`) and Docker Hub for all push events.

#### Scenario: Branch push publishes to both registries
- **WHEN** a commit is pushed to any branch
- **THEN** the Docker image is pushed to `ghcr.io/<owner>/<repo>` and `<dockerhub-user>/fusionn-air`

#### Scenario: Tag push publishes to both registries
- **WHEN** a version tag is pushed
- **THEN** all semver tags and `latest` are pushed to both `ghcr.io/<owner>/<repo>` and `<dockerhub-user>/fusionn-air`
