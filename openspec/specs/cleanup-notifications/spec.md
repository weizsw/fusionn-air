# cleanup-notifications Specification

## Purpose
TBD - created by archiving change improve-unmonitor-logging. Update Purpose after archive.
## Requirements
### Requirement: Persistent Queue Visibility
The cleanup service SHALL show queued media in the QUEUED section of notifications only if they are not ready for removal in the current run. Media items ready for removal SHALL appear only in the REMOVED section.

#### Scenario: Already queued media checked on subsequent runs
- **WHEN** media is already in the queue (regardless of monitor status)
- **AND** the delay period has not passed
- **THEN** it SHALL be returned as "queued" action with updated days remaining

#### Scenario: Series queued across multiple cleanup runs
- **WHEN** a series is queued for deletion on Day 1 and unmonitored
- **AND** cleanup runs again on Day 2
- **AND** the delay period has not passed
- **THEN** it SHALL appear as "queued" with days remaining

#### Scenario: Movie queued across multiple cleanup runs
- **WHEN** a movie is queued for deletion on Day 1 and unmonitored
- **AND** cleanup runs again on Day 2
- **AND** the delay period has not passed
- **THEN** it SHALL appear as "queued" with days remaining

#### Scenario: Queued item becomes ready for removal
- **WHEN** a media item has been in queue for the full delay period
- **AND** cleanup runs after the delay period
- **THEN** the item SHALL NOT appear in the "queued" section
- **AND** the item SHALL appear only in the "removed" section
- **AND** notifications SHALL show the item once under "removed"

### Requirement: Contextual Unmonitor Notifications
The cleanup service SHALL include unmonitor context in notification messages when media is queued for deletion and unmonitored.

#### Scenario: Series newly queued and unmonitored notification
- **WHEN** a series is newly queued for deletion and successfully unmonitored
- **THEN** the notification detail reason SHALL indicate both actions:
  - Use "queued for deletion (unmonitored)" instead of "added to queue"
  - Example: "fully watched (S01,S02) - queued for deletion (unmonitored)"

#### Scenario: Movie newly queued and unmonitored notification
- **WHEN** a movie is newly queued for deletion and successfully unmonitored
- **THEN** the notification detail reason SHALL indicate both actions:
  - Use "queued for deletion (unmonitored)" instead of "added to queue"
  - Example: "watched 2024-12-20 - queued for deletion (unmonitored)"

#### Scenario: Already queued media in subsequent notifications
- **WHEN** media is already queued and still in the queue
- **THEN** the notification reason SHALL maintain "queued for deletion (unmonitored)" text
- **AND** the days until removal SHALL be updated

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

