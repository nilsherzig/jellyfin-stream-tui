package jellyfin

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Positive: ticks <-> seconds round-trip correctly.
func TestTicksRoundtrip(t *testing.T) {
	if got := SecondsToTicks(5); got != 50_000_000 {
		t.Fatalf("SecondsToTicks(5) = %d, want 50000000", got)
	}
	if got := TicksToSeconds(50_000_000); got != 5 {
		t.Fatalf("TicksToSeconds = %v, want 5", got)
	}
}

// Positive: a successful auth stores token and user ID and sends Username/Pw.
func TestAuthenticate_Success(t *testing.T) {
	var gotBody map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/Users/AuthenticateByName" {
			t.Errorf("wrong path: %s", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &gotBody)
		json.NewEncoder(w).Encode(authResult{AccessToken: "tok123", User: struct {
			ID   string `json:"Id"`
			Name string `json:"Name"`
		}{ID: "user42", Name: "nils"}})
	}))
	defer srv.Close()

	c := New(srv.URL, "dev1")
	if err := c.Authenticate("nils", "test"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.token != "tok123" || c.userID != "user42" {
		t.Fatalf("token/userID not set: %q %q", c.token, c.userID)
	}
	if gotBody["Username"] != "nils" || gotBody["Pw"] != "test" {
		t.Fatalf("wrong body sent: %+v", gotBody)
	}
}

// Negative: HTTP 401 must return an error and leave the token empty.
func TestAuthenticate_Failure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	c := New(srv.URL, "dev1")
	if err := c.Authenticate("nils", "wrong"); err == nil {
		t.Fatal("expected error for 401, got nil")
	}
	if c.token != "" {
		t.Fatalf("token should be empty, is %q", c.token)
	}
}

// Positive: Children sends ParentId and parses the item list.
func TestChildren_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("ParentId"); got != "parent9" {
			t.Errorf("ParentId = %q, want parent9", got)
		}
		json.NewEncoder(w).Encode(itemsResponse{Items: []Item{
			{ID: "a", Name: "Folder", IsFolder: true},
			{ID: "b", Name: "Movie", Type: "Movie"},
		}})
	}))
	defer srv.Close()

	c := New(srv.URL, "dev1")
	c.userID = "u1"
	items, err := c.Children("parent9")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) != 2 || items[0].Name != "Folder" || !items[0].IsFolder {
		t.Fatalf("wrong items: %+v", items)
	}
}

// Negative: a server error (500) while listing must return an error.
func TestChildren_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := New(srv.URL, "dev1")
	c.userID = "u1"
	if _, err := c.Children("p"); err == nil {
		t.Fatal("expected error for HTTP 500, got nil")
	}
}

// Positive: Resume calls the /Resume endpoint and parses the items.
func TestResume_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/Items/Resume") {
			t.Errorf("wrong path: %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(itemsResponse{Items: []Item{{ID: "x", Name: "Started"}}})
	}))
	defer srv.Close()

	c := New(srv.URL, "dev1")
	c.userID = "u1"
	items, err := c.Resume()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) != 1 || items[0].Name != "Started" {
		t.Fatalf("wrong items: %+v", items)
	}
}

// Positive: StreamURL contains the item ID and token.
func TestStreamURL(t *testing.T) {
	c := New("https://jf.example.com", "dev1")
	c.token = "abc"
	got := c.StreamURL("item7")
	if !strings.Contains(got, "/Videos/item7/stream") || !strings.Contains(got, "api_key=abc") {
		t.Fatalf("unexpected StreamURL: %s", got)
	}
}

// Positive: SubtitleURL builds the correct path with item, media source, index,
// format, and api_key.
func TestSubtitleURL(t *testing.T) {
	c := New("https://jf.example.com", "dev1")
	c.token = "tok"
	got := c.SubtitleURL("item1", "ms42", 3, "srt")
	wantPrefix := "https://jf.example.com/Videos/item1/ms42/Subtitles/3/Stream.srt?api_key=tok"
	if got != wantPrefix {
		t.Fatalf("SubtitleURL = %q, want %q", got, wantPrefix)
	}
}

// Positive: GetPlaybackInfo fetches and parses the PlaybackInfo response.
func TestGetPlaybackInfo_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/Items/item1/PlaybackInfo" {
			t.Errorf("wrong path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("userId") != "u1" {
			t.Errorf("wrong userId: %s", r.URL.Query().Get("userId"))
		}
		json.NewEncoder(w).Encode(PlaybackInfoResponse{
			PlaySessionID: "ps1",
			MediaSources: []MediaSourceInfo{
				{
					ID:   "ms1",
					Name: "Default",
					MediaStreams: []MediaStream{
						{Index: 0, Type: "Video", Codec: "h264"},
						{Index: 1, Type: "Audio", Codec: "aac", Language: "eng"},
						{Index: 2, Type: "Subtitle", Codec: "srt", Language: "eng", IsDefault: true, DisplayTitle: "English"},
						{Index: 3, Type: "Subtitle", Codec: "srt", Language: "fre", DisplayTitle: "French"},
					},
				},
			},
		})
	}))
	defer srv.Close()

	c := New(srv.URL, "dev1")
	c.userID = "u1"
	info, err := c.GetPlaybackInfo("item1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(info.MediaSources) != 1 {
		t.Fatalf("expected 1 media source, got %d", len(info.MediaSources))
	}
	if len(info.MediaSources[0].MediaStreams) != 4 {
		t.Fatalf("expected 4 streams, got %d", len(info.MediaSources[0].MediaStreams))
	}
	// Verify subtitle stream.
	sub := info.MediaSources[0].MediaStreams[2]
	if sub.Type != "Subtitle" || sub.Language != "eng" || !sub.IsDefault {
		t.Fatalf("wrong subtitle stream: %+v", sub)
	}
}

// Negative: GetPlaybackInfo on server error returns an error.
func TestGetPlaybackInfo_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := New(srv.URL, "dev1")
	c.userID = "u1"
	if _, err := c.GetPlaybackInfo("item1"); err == nil {
		t.Fatal("expected error for HTTP 500, got nil")
	}
}

// Positive: ReportProgress posts ItemId, PositionTicks and IsPaused correctly.
func TestReportProgress(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/Sessions/Playing/Progress" {
			t.Errorf("wrong path: %s", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &gotBody)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := New(srv.URL, "dev1")
	c.token = "tok"
	if err := c.ReportProgress("item7", 50_000_000, true); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotBody["ItemId"] != "item7" {
		t.Fatalf("wrong ItemId: %+v", gotBody)
	}
	if gotBody["IsPaused"] != true {
		t.Fatalf("wrong IsPaused: %+v", gotBody)
	}
	// JSON numbers decode as float64.
	if gotBody["PositionTicks"].(float64) != 50_000_000 {
		t.Fatalf("wrong PositionTicks: %+v", gotBody)
	}
}

// Positive: ReportStart creates a PlaySessionId that ReportProgress reuses
// (without it Jellyfin discards the position).
func TestPlaySessionID_LinksReports(t *testing.T) {
	var startSID, progressSID string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		raw, _ := io.ReadAll(r.Body)
		json.Unmarshal(raw, &body)
		sid, _ := body["PlaySessionId"].(string)
		if strings.HasSuffix(r.URL.Path, "/Playing") {
			startSID = sid
		} else {
			progressSID = sid
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := New(srv.URL, "dev1")
	c.token = "tok"
	if err := c.ReportStart("item7"); err != nil {
		t.Fatalf("ReportStart: %v", err)
	}
	if err := c.ReportProgress("item7", 1, false); err != nil {
		t.Fatalf("ReportProgress: %v", err)
	}
	if startSID == "" {
		t.Fatal("ReportStart should send a PlaySessionId")
	}
	if startSID != progressSID {
		t.Fatalf("session IDs differ: %q vs %q", startSID, progressSID)
	}
}
