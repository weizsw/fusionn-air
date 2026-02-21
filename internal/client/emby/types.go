package emby

import "strconv"

type ItemsResponse struct {
	Items            []Item `json:"Items"`
	TotalRecordCount int    `json:"TotalRecordCount"`
}

type Item struct {
	ID           string      `json:"Id"`
	Name         string      `json:"Name"`
	Type         string      `json:"Type"`
	Path         string      `json:"Path"`
	ParentID     string      `json:"ParentId"`
	ProviderIDs  ProviderIDs `json:"ProviderIds"`
	IndexNumber  int         `json:"IndexNumber"`
	LocationType string      `json:"LocationType"`
	IsFolder     bool        `json:"IsFolder"`
}

type VirtualFolder struct {
	Name           string `json:"Name"`
	ItemID         string `json:"ItemId"`
	CollectionType string `json:"CollectionType"`
}

type ProviderIDs struct {
	Tvdb string `json:"Tvdb"`
	Tmdb string `json:"Tmdb"`
	Imdb string `json:"Imdb"`
}

func ParseProviderID(ids ProviderIDs, key string) int {
	var val string
	switch key {
	case "Tvdb":
		val = ids.Tvdb
	case "Tmdb":
		val = ids.Tmdb
	default:
		return 0
	}
	if val == "" {
		return 0
	}
	id, err := strconv.Atoi(val)
	if err != nil {
		return 0
	}
	return id
}
