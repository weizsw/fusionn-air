package overseerr

// MediaType for Overseerr API
type MediaType string

const (
	MediaTypeTV    MediaType = "tv"
	MediaTypeMovie MediaType = "movie"
)

// MediaStatus represents the status of media in Overseerr
type MediaStatus int

const (
	MediaStatusUnknown        MediaStatus = 1
	MediaStatusPending        MediaStatus = 2
	MediaStatusProcessing     MediaStatus = 3
	MediaStatusPartiallyAvail MediaStatus = 4
	MediaStatusAvailable      MediaStatus = 5
)

// RequestStatus represents request status
type RequestStatus int

const (
	RequestStatusPending  RequestStatus = 1
	RequestStatusApproved RequestStatus = 2
	RequestStatusDeclined RequestStatus = 3
)

// SearchResult from Overseerr search API
type SearchResult struct {
	Page         int           `json:"page"`
	TotalPages   int           `json:"totalPages"`
	TotalResults int           `json:"totalResults"`
	Results      []MediaResult `json:"results"`
}

type MediaResult struct {
	ID            int        `json:"id"`
	MediaType     string     `json:"mediaType"`
	Name          string     `json:"name,omitempty"`  // TV shows
	Title         string     `json:"title,omitempty"` // Movies
	OriginalName  string     `json:"originalName,omitempty"`
	OriginalTitle string     `json:"originalTitle,omitempty"`
	Overview      string     `json:"overview"`
	PosterPath    string     `json:"posterPath"`
	BackdropPath  string     `json:"backdropPath"`
	FirstAirDate  string     `json:"firstAirDate,omitempty"`
	ReleaseDate   string     `json:"releaseDate,omitempty"`
	MediaInfo     *MediaInfo `json:"mediaInfo,omitempty"`
}

type MediaInfo struct {
	ID       int          `json:"id"`
	TMDBID   int          `json:"tmdbId"`
	TVDBID   int          `json:"tvdbId,omitempty"`
	Status   MediaStatus  `json:"status"`
	Requests []Request    `json:"requests,omitempty"`
	Seasons  []SeasonInfo `json:"seasons,omitempty"`
}

type SeasonInfo struct {
	ID           int         `json:"id"`
	SeasonNumber int         `json:"seasonNumber"`
	Status       MediaStatus `json:"status"`
}

type Request struct {
	ID          int           `json:"id"`
	Status      RequestStatus `json:"status"`
	Media       *MediaInfo    `json:"media,omitempty"`
	Seasons     []SeasonReq   `json:"seasons,omitempty"`
	RequestedBy *User         `json:"requestedBy,omitempty"`
	CreatedAt   string        `json:"createdAt"`
}

type SeasonReq struct {
	ID           int `json:"id"`
	SeasonNumber int `json:"seasonNumber"`
}

type User struct {
	ID          int    `json:"id"`
	Email       string `json:"email"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
}

// TVRequest is the payload to request a TV show
type TVRequest struct {
	MediaType string `json:"mediaType"`
	MediaID   int    `json:"mediaId"` // TMDB ID
	Seasons   []int  `json:"seasons"`
	UserID    int    `json:"userId,omitempty"` // Request as specific user
}

// TVDetails from Overseerr
type TVDetails struct {
	ID               int        `json:"id"`
	Name             string     `json:"name"`
	NumberOfSeasons  int        `json:"numberOfSeasons"`
	NumberOfEpisodes int        `json:"numberOfEpisodes"`
	Seasons          []TVSeason `json:"seasons"`
	MediaInfo        *MediaInfo `json:"mediaInfo,omitempty"`
}

type TVSeason struct {
	ID           int    `json:"id"`
	SeasonNumber int    `json:"seasonNumber"`
	EpisodeCount int    `json:"episodeCount"`
	AirDate      string `json:"airDate"`
	Name         string `json:"name"`
}

// RequestResponse after creating a request
type RequestResponse struct {
	ID        int    `json:"id"`
	Status    int    `json:"status"`
	CreatedAt string `json:"createdAt"`
}
