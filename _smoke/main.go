// Manual end-to-end smoke test against a real Jellyfin server.
// It lives in _smoke/ (ignored by the Go toolchain) and runs manually only.
// Server and credentials come from the config (default config.yaml, or JFTUI_CONFIG):
//
//	JFTUI_MPV_ARGS="--vo=null --no-video --ao=null --length=4" go run ./_smoke
//
// It checks: login → browse → mpv playback with position reporting to Jellyfin.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/nilsherzig/jellyfin-stream-tui/internal/config"
	"github.com/nilsherzig/jellyfin-stream-tui/internal/jellyfin"
	"github.com/nilsherzig/jellyfin-stream-tui/internal/player"
)

func main() {
	// Config path from JFTUI_CONFIG, otherwise $HOME/.config/jellyfin-stream-tui/config.yaml.
	cfgPath := os.Getenv("JFTUI_CONFIG")
	if cfgPath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			panic(fmt.Errorf("cannot determine home directory: %w", err))
		}
		cfgPath = filepath.Join(home, ".config", "jellyfin-stream-tui", "config.yaml")
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		panic(err)
	}

	c := jellyfin.New(cfg.Server, "smoke-test")
	if err := c.Authenticate(cfg.Username, cfg.Password); err != nil {
		panic(err)
	}
	fmt.Println("✓ login ok")

	views, err := c.Views()
	if err != nil {
		panic(err)
	}
	fmt.Printf("✓ %d views\n", len(views))

	// Find the first playable episode/movie via ParentId navigation.
	item := findPlayable(c, views, 0)
	if item == nil {
		panic("no playable item found")
	}
	fmt.Printf("✓ playing: %s (%s), resume at %.0fs\n",
		item.Name, item.Type, jellyfin.TicksToSeconds(item.UserData.PlaybackPositionTicks))

	_ = c.ReportStart(item.ID)
	socket := filepath.Join(os.TempDir(), "jftui-smoke.sock")
	var lastReported float64
	finalPos, err := player.New(socket).Play(c.StreamURL(item.ID), 0, func(s player.Status) {
		lastReported = s.Position
		fmt.Printf("  → pos=%.1fs paused=%v\n", s.Position, s.Paused)
		_ = c.ReportProgress(item.ID, jellyfin.SecondsToTicks(s.Position), s.Paused)
	})
	if err != nil {
		fmt.Println("mpv exit:", err)
	}
	_ = c.ReportStopped(item.ID, jellyfin.SecondsToTicks(finalPos))
	fmt.Printf("✓ final position: %.1fs (last reported: %.1fs)\n", finalPos, lastReported)

	// Verify the server took the position.
	refreshed := findByID(c, views, item.ID)
	if refreshed != nil {
		fmt.Printf("✓ server position now: %.1fs\n",
			jellyfin.TicksToSeconds(refreshed.UserData.PlaybackPositionTicks))
	}
}

// findPlayable descends recursively via ParentId until it finds a non-folder.
func findPlayable(c *jellyfin.Client, items []jellyfin.Item, depth int) *jellyfin.Item {
	if depth > 6 {
		return nil
	}
	for _, it := range items {
		if !it.IsFolder {
			return &it
		}
		kids, err := c.Children(it.ID)
		if err != nil {
			continue
		}
		if found := findPlayable(c, kids, depth+1); found != nil {
			return found
		}
	}
	return nil
}

func findByID(c *jellyfin.Client, views []jellyfin.Item, id string) *jellyfin.Item {
	for _, v := range views {
		kids, err := c.Children(v.ID)
		if err != nil {
			continue
		}
		for i := range kids {
			if kids[i].ID == id {
				return &kids[i]
			}
		}
	}
	return nil
}
