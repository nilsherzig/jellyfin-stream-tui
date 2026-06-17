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
	cfgPath := flag.String("config", "", "path to the YAML config (default: $HOME/.config/jellyfin-stream-tui/config.yaml)")
	flag.Parse()

	if err := run(*cfgPath); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

func resolveConfigPath(explicitPath string) (string, error) {
	if explicitPath != "" {
		return explicitPath, nil
	}
	if p := os.Getenv("JFTUI_CONFIG"); p != "" {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return filepath.Join(home, ".config", "jellyfin-stream-tui", "config.yaml"), nil
}

func run(cfgPath string) error {
	path, err := resolveConfigPath(cfgPath)
	if err != nil {
		return err
	}
	cfg, err := config.Load(path)
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

// pickSubtitles fetches playback info and returns subtitle URLs to pass to mpv.
// It respects JFTUI_SUBTITLE_LANG (language code, e.g. "eng") to select a
// specific language. Set JFTUI_SUBTITLE_LANG=off to disable subtitles entirely.
func (s *session) pickSubtitles(itemID string) []string {
	lang := os.Getenv("JFTUI_SUBTITLE_LANG")
	if lang == "off" {
		return nil
	}

	info, err := s.client.GetPlaybackInfo(itemID)
	if err != nil {
		return nil // no subtitles on error
	}

	// Collect subtitle streams from all media sources.
	type subStream struct {
		itemID        string
		mediaSourceID string
		index         int
		language      string
		isDefault     bool
		isForced      bool
	}
	var subs []subStream
	for _, ms := range info.MediaSources {
		for _, stream := range ms.MediaStreams {
			if stream.Type != "Subtitle" {
				continue
			}
			subs = append(subs, subStream{
				itemID:        itemID,
				mediaSourceID: ms.ID,
				index:         stream.Index,
				language:      stream.Language,
				isDefault:     stream.IsDefault,
				isForced:      stream.IsForced,
			})
		}
	}

	if len(subs) == 0 {
		return nil
	}

	// If a language preference is set, find a matching stream.
	if lang != "" {
		for _, sub := range subs {
			if sub.language == lang {
				return []string{s.client.SubtitleURL(sub.itemID, sub.mediaSourceID, sub.index, "srt")}
			}
		}
		return nil // language not found
	}

	// No language preference: pick the default or forced subtitle.
	for _, sub := range subs {
		if sub.isDefault || sub.isForced {
			return []string{s.client.SubtitleURL(sub.itemID, sub.mediaSourceID, sub.index, "srt")}
		}
	}

	// Fall back to the first subtitle stream.
	return []string{s.client.SubtitleURL(subs[0].itemID, subs[0].mediaSourceID, subs[0].index, "srt")}
}

// playItem starts mpv and reports start/progress/stop to Jellyfin.
func (s *session) playItem(it jellyfin.Item) error {
	streamURL := s.client.StreamURL(it.ID)
	resume := jellyfin.TicksToSeconds(it.UserData.PlaybackPositionTicks)

	subtitleFiles := s.pickSubtitles(it.ID)

	_ = s.client.ReportStart(it.ID)

	finalPos, err := s.player.Play(streamURL, resume, subtitleFiles, func(st player.Status) {
		// Reporting errors are not fatal to playback.
		_ = s.client.ReportProgress(it.ID, jellyfin.SecondsToTicks(st.Position), st.Paused)
	})

	_ = s.client.ReportStopped(it.ID, jellyfin.SecondsToTicks(finalPos))
	return err
}
