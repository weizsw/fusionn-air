# cleanup-logging Specification

## Purpose
TBD - created by archiving change improve-unmonitor-logging. Update Purpose after archive.
## Requirements
### Requirement: Contextual Unmonitor Notifications
The cleanup service SHALL include unmonitor context in notification messages when media is queued for deletion and unmonitored.

#### Scenario: Series queued and unmonitored notification
- **WHEN** a series is queued for deletion and successfully unmonitored
- **THEN** the notification detail reason SHALL indicate both actions by:
  - Replacing "added to queue" with "queued for deletion (unmonitored)"
  - Example: "fully watched (S01,S02) - queued for deletion (unmonitored)"

#### Scenario: Movie queued and unmonitored notification
- **WHEN** a movie is queued for deletion and successfully unmonitored
- **THEN** the notification detail reason SHALL indicate both actions by:
  - Replacing "added to queue" with "queued for deletion (unmonitored)"
  - Example: "watched 2024-12-20 - queued for deletion (unmonitored)"

#### Scenario: Dry-run mode notification
- **WHEN** media would be unmonitored in dry-run mode
- **THEN** the notification reason SHALL still indicate "(unmonitored)" context
- **AND** the reason text SHALL clarify the hypothetical nature

### Requirement: Contextual Unmonitor Logging
The cleanup service SHALL log unmonitor operations with sufficient context to explain why media is being unmonitored.

#### Scenario: Dry-run unmonitor logging for series
- **WHEN** a series is unmonitored in dry-run mode because it's queued for deletion
- **THEN** the log message SHALL include:
  - Dry-run indicator (`[DRY RUN]`)
  - Action description ("Would unmonitor series")
  - Media title
  - Context explaining it's queued for deletion (e.g., "(queued for deletion)")

#### Scenario: Actual unmonitor logging for series
- **WHEN** a series is successfully unmonitored because it's queued for deletion
- **THEN** the log message SHALL include:
  - Action description (e.g., "Unmonitored series")
  - Media title
  - Context explaining it's queued for deletion (e.g., "(queued for deletion)")

#### Scenario: Failed unmonitor logging
- **WHEN** unmonitoring fails
- **THEN** the warning message SHALL include:
  - Warning indicator (`⚠️`)
  - Failure description
  - Media title
  - Error details

#### Scenario: Dry-run unmonitor logging for movies
- **WHEN** a movie is unmonitored in dry-run mode because it's queued for deletion
- **THEN** the log message SHALL include:
  - Dry-run indicator (`[DRY RUN]`)
  - Action description ("Would unmonitor movie")
  - Media title
  - Context explaining it's queued for deletion (e.g., "(queued for deletion)")

#### Scenario: Actual unmonitor logging for movies
- **WHEN** a movie is successfully unmonitored because it's queued for deletion
- **THEN** the log message SHALL include:
  - Action description (e.g., "Unmonitored movie")
  - Media title
  - Context explaining it's queued for deletion (e.g., "(queued for deletion)")

