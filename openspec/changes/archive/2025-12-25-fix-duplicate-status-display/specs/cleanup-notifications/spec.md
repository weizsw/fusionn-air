## MODIFIED Requirements

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

