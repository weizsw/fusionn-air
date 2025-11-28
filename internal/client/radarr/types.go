package radarr

import "time"

// Movie represents a movie in Radarr
type Movie struct {
	ID               int        `json:"id"`
	Title            string     `json:"title"`
	OriginalTitle    string     `json:"originalTitle"`
	SortTitle        string     `json:"sortTitle"`
	Status           string     `json:"status"` // "released", "announced", "inCinemas"
	Overview         string     `json:"overview"`
	Year             int        `json:"year"`
	Path             string     `json:"path"`
	TmdbID           int        `json:"tmdbId"`
	ImdbID           string     `json:"imdbId"`
	Monitored        bool       `json:"monitored"`
	HasFile          bool       `json:"hasFile"`
	IsAvailable      bool       `json:"isAvailable"`
	FolderName       string     `json:"folderName"`
	Runtime          int        `json:"runtime"`
	CleanTitle       string     `json:"cleanTitle"`
	TitleSlug        string     `json:"titleSlug"`
	Certification    string     `json:"certification"`
	Genres           []string   `json:"genres"`
	Tags             []int      `json:"tags"`
	Added            time.Time  `json:"added"`
	Ratings          Rating     `json:"ratings"`
	MovieFile        *MovieFile `json:"movieFile,omitempty"`
	QualityProfileID int        `json:"qualityProfileId"`
	SizeOnDisk       int64      `json:"sizeOnDisk"`
	DigitalRelease   string     `json:"digitalRelease,omitempty"`
	PhysicalRelease  string     `json:"physicalRelease,omitempty"`
	InCinemas        string     `json:"inCinemas,omitempty"`
}

type Rating struct {
	Votes int     `json:"votes"`
	Value float64 `json:"value"`
}

// MovieFile represents the file for a movie in Radarr
type MovieFile struct {
	ID               int       `json:"id"`
	MovieID          int       `json:"movieId"`
	RelativePath     string    `json:"relativePath"`
	Path             string    `json:"path"`
	Size             int64     `json:"size"`
	DateAdded        time.Time `json:"dateAdded"`
	Quality          Quality   `json:"quality"`
	MediaInfo        MediaInfo `json:"mediaInfo"`
	OriginalFilePath string    `json:"originalFilePath"`
}

type Quality struct {
	Quality struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	} `json:"quality"`
}

type MediaInfo struct {
	VideoBitDepth  int     `json:"videoBitDepth"`
	VideoCodec     string  `json:"videoCodec"`
	VideoFps       float64 `json:"videoFps"`
	Resolution     string  `json:"resolution"`
	RunTime        string  `json:"runTime"`
	AudioChannels  float64 `json:"audioChannels"`
	AudioCodec     string  `json:"audioCodec"`
	AudioLanguages string  `json:"audioLanguages"`
	Subtitles      string  `json:"subtitles"`
}

// DeleteOptions for removing a movie
type DeleteOptions struct {
	DeleteFiles        bool `json:"deleteFiles"`
	AddImportExclusion bool `json:"addImportExclusion"`
}

// MovieStatus constants
const (
	StatusReleased  = "released"
	StatusAnnounced = "announced"
	StatusInCinemas = "inCinemas"
)
