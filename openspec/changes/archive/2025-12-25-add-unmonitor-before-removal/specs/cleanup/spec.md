## ADDED Requirements

### Requirement: Cleanup Queue Processing
The cleanup service SHALL unmonitor media items immediately when they are added to the removal queue, then delete them after the configured delay period has passed.

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

