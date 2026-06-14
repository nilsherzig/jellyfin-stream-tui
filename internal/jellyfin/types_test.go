package jellyfin

import "testing"

func intp(i int) *int { return &i }

// Positive: an episode with series and S/E formats as "Series → S01E01 Title".
func TestDisplayName_Episode(t *testing.T) {
	it := Item{
		Type:              "Episode",
		Name:              "Secrets",
		SeriesName:        "Dark",
		ParentIndexNumber: intp(1),
		IndexNumber:       intp(1),
	}
	if got := it.DisplayName(); got != "Dark → S01E01 Secrets" {
		t.Fatalf("DisplayName = %q", got)
	}
}

// Positive: an episode without S/E numbers falls back to "Series → Title".
func TestDisplayName_EpisodeWithoutNumbers(t *testing.T) {
	it := Item{Type: "Episode", Name: "Pilot", SeriesName: "Dark"}
	if got := it.DisplayName(); got != "Dark → Pilot" {
		t.Fatalf("DisplayName = %q", got)
	}
}

// Negative: a movie (not an episode) shows only its own name, no arrow.
func TestDisplayName_Movie(t *testing.T) {
	it := Item{Type: "Movie", Name: "The Dark Knight"}
	if got := it.DisplayName(); got != "The Dark Knight" {
		t.Fatalf("DisplayName = %q", got)
	}
}
