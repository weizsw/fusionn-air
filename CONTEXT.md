# Fusionn Air

Fusionn Air automates requesting and cleaning up media based on viewing activity.

## Language

**Watcher**:
The process that identifies eligible upcoming series seasons for automatic request.
_Avoid_: Job, scheduler

**Excluded Series**:
A series the Watcher must skip because it matches a configured excluded genre, has a disallowed Original Language, or lacks metadata needed to evaluate an enabled exclusion.
_Avoid_: Filtered show, blocked content

**Original Language**:
The language in which a series was originally produced, independent of dubbed audio tracks.
_Avoid_: Audio language, available language

**Animated Series**:
A series classified by Trakt under `animation`, `anime`, or `donghua`.
_Avoid_: Cartoon, anime-only series
