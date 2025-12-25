## MODIFIED Requirements

### Requirement: Cleanup Queue Processing
The cleanup service SHALL unmonitor media items immediately when they are added to the removal queue, then delete them after the configured delay period has passed. Each media item SHALL appear under exactly one status category (skipped, queued, or removed) in a single cleanup run.

#### Scenario: Series queued for removal
- **WHEN** a fully watched series is detected
- **THEN** the series is added to the cleanup queue
- **AND** the series is unmonitored in Sonarr immediately
- **AND** the series remains in queue until delay_days have passed

#### Scenario: Movie queued for removal
- **WHEN** a watched movie is detected
- **THEN** the movie is added to the cleanup queue
- **AND** the movie is unmonitored in Radarr immediately
- **AND** the movie remains in queue until delay_days have passed

#### Scenario: Dry run mode
- **WHEN** dry_run is enabled
- **AND** media is queued for removal
- **THEN** the unmonitor action is logged but not executed
- **AND** the deletion action is logged but not executed

#### Scenario: Media removed from queue (more episodes coming)
- **WHEN** a queued series has more episodes detected
- **THEN** the series is removed from the queue
- **AND** the series monitoring state is NOT restored (manual re-monitor required)

#### Scenario: Queued item ready for removal in same run
- **WHEN** a media item is in the queue
- **AND** the delay period has passed (ready for removal)
- **THEN** the item SHALL NOT be reported as "queued" in the results
- **AND** the item SHALL be processed by the removal queue
- **AND** the item SHALL appear only in the "removed" status category

