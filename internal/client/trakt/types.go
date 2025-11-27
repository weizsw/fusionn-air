package trakt

import "time"

// CalendarShow represents an episode from the calendar API
type CalendarShow struct {
	FirstAired time.Time `json:"first_aired"`
	Episode    Episode   `json:"episode"`
	Show       Show      `json:"show"`
}

type Episode struct {
	Season  int    `json:"season"`
	Number  int    `json:"number"`
	Title   string `json:"title"`
	IDs     IDs    `json:"ids"`
	Runtime int    `json:"runtime"`
}

type Show struct {
	Title string `json:"title"`
	Year  int    `json:"year"`
	IDs   IDs    `json:"ids"`
}

type IDs struct {
	Trakt int    `json:"trakt"`
	Slug  string `json:"slug"`
	TVDB  int    `json:"tvdb"`
	IMDB  string `json:"imdb"`
	TMDB  int    `json:"tmdb"`
}

// ShowProgress represents watch progress for a show
type ShowProgress struct {
	Aired         int              `json:"aired"`
	Completed     int              `json:"completed"`
	LastWatchedAt *time.Time       `json:"last_watched_at"`
	ResetAt       *time.Time       `json:"reset_at"`
	Seasons       []SeasonProgress `json:"seasons"`
	HiddenSeasons []Season         `json:"hidden_seasons"`
	NextEpisode   *Episode         `json:"next_episode"`
	LastEpisode   *Episode         `json:"last_episode"`
}

type SeasonProgress struct {
	Number    int               `json:"number"`
	Title     string            `json:"title"`
	Aired     int               `json:"aired"`
	Completed int               `json:"completed"`
	Episodes  []EpisodeProgress `json:"episodes"`
}

type EpisodeProgress struct {
	Number        int        `json:"number"`
	Completed     bool       `json:"completed"`
	LastWatchedAt *time.Time `json:"last_watched_at"`
}

type Season struct {
	Number int `json:"number"`
	IDs    IDs `json:"ids"`
}

// WatchedShow from /users/me/watched/shows
type WatchedShow struct {
	Plays         int             `json:"plays"`
	LastWatchedAt time.Time       `json:"last_watched_at"`
	LastUpdatedAt time.Time       `json:"last_updated_at"`
	ResetAt       *time.Time      `json:"reset_at"`
	Show          Show            `json:"show"`
	Seasons       []WatchedSeason `json:"seasons"`
}

type WatchedSeason struct {
	Number   int              `json:"number"`
	Episodes []WatchedEpisode `json:"episodes"`
}

type WatchedEpisode struct {
	Number        int       `json:"number"`
	Plays         int       `json:"plays"`
	LastWatchedAt time.Time `json:"last_watched_at"`
}
