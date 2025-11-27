package sonarr

import "time"

// Series represents a TV series in Sonarr
type Series struct {
	ID                int        `json:"id"`
	Title             string     `json:"title"`
	SortTitle         string     `json:"sortTitle"`
	Status            string     `json:"status"` // "continuing", "ended", "upcoming"
	Overview          string     `json:"overview"`
	Network           string     `json:"network"`
	Year              int        `json:"year"`
	Path              string     `json:"path"`
	TvdbID            int        `json:"tvdbId"`
	TvMazeID          int        `json:"tvMazeId"`
	ImdbID            string     `json:"imdbId"`
	Monitored         bool       `json:"monitored"`
	SeasonFolder      bool       `json:"seasonFolder"`
	UseSceneNumbering bool       `json:"useSceneNumbering"`
	Runtime           int        `json:"runtime"`
	TvRageID          int        `json:"tvRageId"`
	FirstAired        string     `json:"firstAired"`
	SeriesType        string     `json:"seriesType"`
	CleanTitle        string     `json:"cleanTitle"`
	TitleSlug         string     `json:"titleSlug"`
	Certification     string     `json:"certification"`
	Genres            []string   `json:"genres"`
	Tags              []int      `json:"tags"`
	Added             time.Time  `json:"added"`
	Ratings           Rating     `json:"ratings"`
	Statistics        Statistics `json:"statistics"`
	Seasons           []Season   `json:"seasons"`
	LanguageProfileID int        `json:"languageProfileId"`
	QualityProfileID  int        `json:"qualityProfileId"`
}

type Rating struct {
	Votes int     `json:"votes"`
	Value float64 `json:"value"`
}

type Statistics struct {
	SeasonCount       int     `json:"seasonCount"`
	EpisodeFileCount  int     `json:"episodeFileCount"`
	EpisodeCount      int     `json:"episodeCount"`
	TotalEpisodeCount int     `json:"totalEpisodeCount"`
	SizeOnDisk        int64   `json:"sizeOnDisk"`
	PercentOfEpisodes float64 `json:"percentOfEpisodes"`
}

type Season struct {
	SeasonNumber int               `json:"seasonNumber"`
	Monitored    bool              `json:"monitored"`
	Statistics   *SeasonStatistics `json:"statistics,omitempty"`
}

type SeasonStatistics struct {
	EpisodeFileCount  int     `json:"episodeFileCount"`
	EpisodeCount      int     `json:"episodeCount"`
	TotalEpisodeCount int     `json:"totalEpisodeCount"`
	SizeOnDisk        int64   `json:"sizeOnDisk"`
	PercentOfEpisodes float64 `json:"percentOfEpisodes"`
}

// Episode represents an episode in Sonarr
type Episode struct {
	ID                       int       `json:"id"`
	SeriesID                 int       `json:"seriesId"`
	TvdbID                   int       `json:"tvdbId"`
	EpisodeFileID            int       `json:"episodeFileId"`
	SeasonNumber             int       `json:"seasonNumber"`
	EpisodeNumber            int       `json:"episodeNumber"`
	Title                    string    `json:"title"`
	AirDate                  string    `json:"airDate"`
	AirDateUtc               time.Time `json:"airDateUtc"`
	Overview                 string    `json:"overview"`
	HasFile                  bool      `json:"hasFile"`
	Monitored                bool      `json:"monitored"`
	AbsoluteEpisodeNumber    int       `json:"absoluteEpisodeNumber"`
	UnverifiedSceneNumbering bool      `json:"unverifiedSceneNumbering"`
}

// DeleteOptions for removing a series
type DeleteOptions struct {
	DeleteFiles            bool `json:"deleteFiles"`
	AddImportListExclusion bool `json:"addImportListExclusion"`
}

// SeriesStatus constants
const (
	StatusContinuing = "continuing"
	StatusEnded      = "ended"
	StatusUpcoming   = "upcoming"
)
