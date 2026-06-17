package jellyfin

import "fmt"

// ticksPerSecond: Jellyfin measures time in 100ns "ticks" → 10 million per second.
const ticksPerSecond = 10_000_000

// SecondsToTicks converts seconds to Jellyfin ticks.
func SecondsToTicks(s float64) int64 { return int64(s * ticksPerSecond) }

// TicksToSeconds converts Jellyfin ticks to seconds.
func TicksToSeconds(t int64) float64 { return float64(t) / ticksPerSecond }

// UserData holds an item's per-user playback state.
type UserData struct {
	PlaybackPositionTicks int64   `json:"PlaybackPositionTicks"`
	PlayedPercentage      float64 `json:"PlayedPercentage"`
	Played                bool    `json:"Played"`
}

// Item is an entry in the library (library, series, season, movie, episode …).
type Item struct {
	ID           string   `json:"Id"`
	Name         string   `json:"Name"`
	Type         string   `json:"Type"`
	IsFolder     bool     `json:"IsFolder"`
	RunTimeTicks int64    `json:"RunTimeTicks"`
	UserData     UserData `json:"UserData"`

	// Set on episodes only:
	SeriesName        string `json:"SeriesName"`        // e.g. "Dark"
	ParentIndexNumber *int   `json:"ParentIndexNumber"` // season
	IndexNumber       *int   `json:"IndexNumber"`       // episode
}

// DisplayName builds the display name. For episodes it includes the series and
// S/E, e.g. "Dark → S01E01 Secrets"; otherwise it is just the name.
func (it Item) DisplayName() string {
	if it.Type != "Episode" || it.SeriesName == "" {
		return it.Name
	}
	se := ""
	if it.ParentIndexNumber != nil && it.IndexNumber != nil {
		se = fmt.Sprintf("S%02dE%02d ", *it.ParentIndexNumber, *it.IndexNumber)
	}
	return fmt.Sprintf("%s → %s%s", it.SeriesName, se, it.Name)
}

// MediaStream describes a single stream (video, audio, subtitle, …) inside a
// media source. Only the fields we need are modelled.
type MediaStream struct {
	Index      int    `json:"Index"`
	Type       string `json:"Type"`       // Video, Audio, Subtitle, …
	Codec      string `json:"Codec"`
	Language   string `json:"Language"`
	DisplayTitle string `json:"DisplayTitle"`
	IsDefault  bool   `json:"IsDefault"`
	IsForced   bool   `json:"IsForced"`
	IsExternal bool   `json:"IsExternal"`
	DeliveryUrl string `json:"DeliveryUrl"`
}

// MediaSourceInfo holds one media source (e.g. one file version).
type MediaSourceInfo struct {
	ID           string        `json:"Id"`
	Name         string        `json:"Name"`
	MediaStreams []MediaStream `json:"MediaStreams"`
}

// PlaybackInfoResponse mirrors the response from /Items/{itemId}/PlaybackInfo.
type PlaybackInfoResponse struct {
	MediaSources  []MediaSourceInfo `json:"MediaSources"`
	PlaySessionID string            `json:"PlaySessionId"`
}

// itemsResponse mirrors the Jellyfin API list responses.
type itemsResponse struct {
	Items            []Item `json:"Items"`
	TotalRecordCount int    `json:"TotalRecordCount"`
}

// authResult mirrors the response from /Users/AuthenticateByName.
type authResult struct {
	AccessToken string `json:"AccessToken"`
	User        struct {
		ID   string `json:"Id"`
		Name string `json:"Name"`
	} `json:"User"`
}
