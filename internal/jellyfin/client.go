package jellyfin

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// Client wraps communication with a Jellyfin server.
type Client struct {
	baseURL  string
	deviceID string
	http     *http.Client

	token  string // set after Authenticate
	userID string

	// playSessionID links Start/Progress/Stopped of one playback.
	// Without it, Jellyfin discards the reported position.
	playSessionID string
}

// New creates a Client. deviceID identifies this device to Jellyfin.
func New(baseURL, deviceID string) *Client {
	return &Client{
		baseURL:  baseURL,
		deviceID: deviceID,
		http:     &http.Client{Timeout: 30 * time.Second},
	}
}

// authHeader builds the MediaBrowser authorization header. After login the
// token is included so subsequent requests are authorized.
func (c *Client) authHeader() string {
	h := fmt.Sprintf(`MediaBrowser Client="jellyfin-stream-tui", Device="cli", DeviceId=%q, Version="0.1.0"`, c.deviceID)
	if c.token != "" {
		h += fmt.Sprintf(`, Token=%q`, c.token)
	}
	return h
}

// do runs a request and sets the auth header.
func (c *Client) do(method, path string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequest(method, c.baseURL+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", c.authHeader())
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.http.Do(req)
}

// Authenticate logs in with username/password and stores the token and user ID.
func (c *Client) Authenticate(username, password string) error {
	payload, _ := json.Marshal(map[string]string{"Username": username, "Pw": password})
	resp, err := c.do(http.MethodPost, "/Users/AuthenticateByName", bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("login request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("login failed: HTTP %d", resp.StatusCode)
	}

	var ar authResult
	if err := json.NewDecoder(resp.Body).Decode(&ar); err != nil {
		return fmt.Errorf("decode login response: %w", err)
	}
	c.token = ar.AccessToken
	c.userID = ar.User.ID
	return nil
}

// listItems fetches a list endpoint and parses the items.
func (c *Client) listItems(path string) ([]Item, error) {
	resp, err := c.do(http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("load items: HTTP %d", resp.StatusCode)
	}

	var ir itemsResponse
	if err := json.NewDecoder(resp.Body).Decode(&ir); err != nil {
		return nil, fmt.Errorf("parse items: %w", err)
	}
	return ir.Items, nil
}

// Views returns the user's top-level libraries (Movies, Shows, …).
func (c *Client) Views() ([]Item, error) {
	return c.listItems(fmt.Sprintf("/Users/%s/Views", c.userID))
}

// Children returns a folder's children (Series→Seasons, Season→Episodes …).
// ParentId handles every level uniformly.
func (c *Client) Children(parentID string) ([]Item, error) {
	q := url.Values{}
	q.Set("ParentId", parentID)
	q.Set("Fields", "UserData,RunTimeTicks")
	q.Set("SortBy", "SortName")
	q.Set("SortOrder", "Ascending")
	return c.listItems(fmt.Sprintf("/Users/%s/Items?%s", c.userID, q.Encode()))
}

// Resume returns the "Continue Watching" list (started, unfinished media).
func (c *Client) Resume() ([]Item, error) {
	q := url.Values{}
	q.Set("Fields", "UserData,RunTimeTicks")
	q.Set("MediaTypes", "Video")
	q.Set("Limit", "12")
	return c.listItems(fmt.Sprintf("/Users/%s/Items/Resume?%s", c.userID, q.Encode()))
}

// NextUp returns the next episode for shows that have been started (the
// "Next Up" section on the Jellyfin home screen).
func (c *Client) NextUp() ([]Item, error) {
	q := url.Values{}
	q.Set("Fields", "UserData,RunTimeTicks")
	q.Set("Limit", "12")
	return c.listItems(fmt.Sprintf("/Shows/NextUp?%s", q.Encode()))
}

// StreamURL builds the direct stream URL that mpv can play.
func (c *Client) StreamURL(itemID string) string {
	return fmt.Sprintf("%s/Videos/%s/stream?static=true&api_key=%s", c.baseURL, itemID, url.QueryEscape(c.token))
}

// GetPlaybackInfo fetches live playback media info for an item, including
// available media sources and their streams (video, audio, subtitles).
func (c *Client) GetPlaybackInfo(itemID string) (*PlaybackInfoResponse, error) {
	resp, err := c.do(http.MethodGet, fmt.Sprintf("/Items/%s/PlaybackInfo?userId=%s", itemID, c.userID), nil)
	if err != nil {
		return nil, fmt.Errorf("playback info request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("playback info: HTTP %d", resp.StatusCode)
	}

	var info PlaybackInfoResponse
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, fmt.Errorf("parse playback info: %w", err)
	}
	return &info, nil
}

// SubtitleURL builds a URL to download a specific subtitle stream in the given
// format (e.g. "srt", "vtt", "ass").
func (c *Client) SubtitleURL(itemID, mediaSourceID string, index int, format string) string {
	return fmt.Sprintf("%s/Videos/%s/%s/Subtitles/%d/Stream.%s?api_key=%s",
		c.baseURL, itemID, mediaSourceID, index, format, url.QueryEscape(c.token))
}

// postReport sends a playback report to Jellyfin.
func (c *Client) postReport(path string, body map[string]any) error {
	payload, _ := json.Marshal(body)
	resp, err := c.do(http.MethodPost, path, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("%s: HTTP %d", path, resp.StatusCode)
	}
	return nil
}

// ReportStart reports the start of playback and creates a new PlaySessionId
// that links all later reports of this playback.
func (c *Client) ReportStart(itemID string) error {
	c.playSessionID = newSessionID()
	return c.postReport("/Sessions/Playing", map[string]any{
		"ItemId":        itemID,
		"PlayMethod":    "DirectPlay",
		"PlaySessionId": c.playSessionID,
	})
}

// ReportProgress updates the playback position on the server.
func (c *Client) ReportProgress(itemID string, positionTicks int64, paused bool) error {
	return c.postReport("/Sessions/Playing/Progress", map[string]any{
		"ItemId":        itemID,
		"PositionTicks": positionTicks,
		"IsPaused":      paused,
		"PlayMethod":    "DirectPlay",
		"PlaySessionId": c.playSessionID,
	})
}

// ReportStopped reports the end of playback with the final position.
func (c *Client) ReportStopped(itemID string, positionTicks int64) error {
	return c.postReport("/Sessions/Playing/Stopped", map[string]any{
		"ItemId":        itemID,
		"PositionTicks": positionTicks,
		"PlaySessionId": c.playSessionID,
	})
}

// newSessionID generates a random, unique session ID.
func newSessionID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
