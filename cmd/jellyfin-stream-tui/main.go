package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/nilsherzig/jellyfin-stream-tui/internal/config"
	"github.com/nilsherzig/jellyfin-stream-tui/internal/jellyfin"
	"github.com/nilsherzig/jellyfin-stream-tui/internal/player"
	"github.com/nilsherzig/jellyfin-stream-tui/internal/tui"
)

func main() {
	cfgPath := flag.String("config", "config.yaml", "path to the YAML config")
	flag.Parse()

	if err := run(*cfgPath); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

func run(cfgPath string) error {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return err
	}

	// Stable device ID per user so Jellyfin can group the sessions.
	client := jellyfin.New(cfg.Server, "jellyfin-stream-tui-"+cfg.Username)
	if err := client.Authenticate(cfg.Username, cfg.Password); err != nil {
		return err
	}

	socket := filepath.Join(os.TempDir(), "jellyfin-stream-tui-mpv.sock")
	s := &session{client: client, player: player.New(socket)}
	model := tui.New(client, s.play)

	// AltScreen: own screen buffer, restored cleanly on exit.
	_, err = tea.NewProgram(model, tea.WithAltScreen()).Run()

	// On quit: kill mpv if it is still playing, then wait for the in-flight
	// playback to send its final Stopped report. Otherwise mpv would keep
	// streaming orphaned and the position would never sync.
	s.player.Stop()
	s.wg.Wait()
	return err
}

// session owns the single player and tracks the in-flight playback so it can
// be torn down cleanly when the TUI quits.
type session struct {
	client *jellyfin.Client
	player *player.Player
	wg     sync.WaitGroup
}

// play wires TUI ↔ player ↔ Jellyfin reporting.
func (s *session) play(it jellyfin.Item) tea.Cmd {
	return func() tea.Msg {
		s.wg.Add(1)
		defer s.wg.Done()
		return tui.PlayDoneMsg{Err: s.playItem(it)}
	}
}

// playItem starts mpv and reports start/progress/stop to Jellyfin.
func (s *session) playItem(it jellyfin.Item) error {
	streamURL := s.client.StreamURL(it.ID)
	resume := jellyfin.TicksToSeconds(it.UserData.PlaybackPositionTicks)

	_ = s.client.ReportStart(it.ID)

	finalPos, err := s.player.Play(streamURL, resume, func(st player.Status) {
		// Reporting errors are not fatal to playback.
		_ = s.client.ReportProgress(it.ID, jellyfin.SecondsToTicks(st.Position), st.Paused)
	})

	_ = s.client.ReportStopped(it.ID, jellyfin.SecondsToTicks(finalPos))
	return err
}
