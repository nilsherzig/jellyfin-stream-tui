package player

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// Status is a snapshot of playback.
type Status struct {
	Position float64 // seconds since start
	Paused   bool
}

// Player starts mpv and reads the position over mpv's JSON IPC socket.
type Player struct {
	mpvPath    string
	socketPath string

	mu  sync.Mutex
	cmd *exec.Cmd // the running mpv process, nil when idle
}

// New creates a Player that talks to mpv over socketPath.
func New(socketPath string) *Player {
	return &Player{mpvPath: "mpv", socketPath: socketPath}
}

// mpvArgs builds the mpv command line.
func (p *Player) mpvArgs(streamURL string, startSeconds float64) []string {
	args := []string{
		"--input-ipc-server=" + p.socketPath,
		"--no-terminal",      // mpv must not touch the terminal (the TUI owns it)
		"--force-window=yes", // always show a window, even while buffering
	}
	if startSeconds > 0 {
		// mpv expects seconds; whole seconds are enough for resume.
		args = append(args, fmt.Sprintf("--start=%d", int(startSeconds)))
	}
	// Optional extra mpv flags from the environment (e.g. subtitles, audio, or
	// headless flags for testing). Whitespace-separated, later = higher priority.
	if extra := strings.Fields(os.Getenv("JFTUI_MPV_ARGS")); len(extra) > 0 {
		args = append(args, extra...)
	}
	return append(args, streamURL)
}

// ipcResponse mirrors a reply to a get_property command.
// Error is a pointer so we can tell command replies (which have "error") from
// unsolicited event lines (which do not).
type ipcResponse struct {
	Data  json.RawMessage `json:"data"`
	Error *string         `json:"error"`
}

// parseTimePos reads the position from one mpv IPC reply line.
func parseTimePos(line []byte) (float64, error) {
	var r ipcResponse
	if err := json.Unmarshal(line, &r); err != nil {
		return 0, err
	}
	if r.Error == nil || *r.Error != "success" {
		return 0, fmt.Errorf("mpv ipc: no valid position")
	}
	var pos float64
	if err := json.Unmarshal(r.Data, &pos); err != nil {
		return 0, err
	}
	return pos, nil
}

// readReply reads lines until a real command reply (with "error") arrives,
// skipping mpv event lines along the way.
func readReply(reader *bufio.Reader) (*ipcResponse, error) {
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			return nil, err
		}
		var r ipcResponse
		if json.Unmarshal(line, &r) != nil {
			continue
		}
		if r.Error == nil {
			continue // event line → ignore
		}
		return &r, nil
	}
}

// getProperty queries an mpv property and returns the raw reply.
func getProperty(conn net.Conn, reader *bufio.Reader, prop string) (*ipcResponse, error) {
	cmd := fmt.Sprintf(`{"command":["get_property",%q]}`+"\n", prop)
	if _, err := conn.Write([]byte(cmd)); err != nil {
		return nil, err
	}
	return readReply(reader)
}

// tracker holds the last known position thread-safely.
type tracker struct {
	mu  sync.Mutex
	pos float64
}

func (t *tracker) set(v float64) { t.mu.Lock(); t.pos = v; t.mu.Unlock() }
func (t *tracker) get() float64  { t.mu.Lock(); defer t.mu.Unlock(); return t.pos }

// Play starts mpv and blocks until playback ends.
// It calls onProgress periodically while playing.
// It returns the last known position (for the final Stopped report).
func (p *Player) Play(streamURL string, startSeconds float64, onProgress func(Status)) (float64, error) {
	// Remove a stale socket so the dial reconnects cleanly.
	_ = os.Remove(p.socketPath)

	cmd := exec.Command(p.mpvPath, p.mpvArgs(streamURL, startSeconds)...)
	if err := cmd.Start(); err != nil {
		return 0, fmt.Errorf("start mpv: %w", err)
	}
	// Expose the process so Stop can kill it when the TUI quits.
	p.mu.Lock()
	p.cmd = cmd
	p.mu.Unlock()

	tr := &tracker{pos: startSeconds}
	stop := make(chan struct{})
	go p.poll(stop, tr, onProgress)

	err := cmd.Wait() // blocks until the mpv window closes (or Stop kills it)
	close(stop)

	p.mu.Lock()
	p.cmd = nil
	p.mu.Unlock()
	return tr.get(), err
}

// Stop kills the running mpv process, if any. Play then unblocks and reports
// the final position. Calling Stop while idle is a no-op.
func (p *Player) Stop() {
	p.mu.Lock()
	cmd := p.cmd
	p.mu.Unlock()
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}

// reportEveryTicks throttles progress reports to the server. poll queries mpv
// every second (to keep the final position accurate and notice mpv exiting),
// but only forwards a report every reportEveryTicks ticks → one per 10 seconds.
const reportEveryTicks = 10

// shouldReport reports whether the given poll tick count should emit a progress
// report. ticks counts only successful position reads (buffering ticks skipped).
func shouldReport(ticks int) bool {
	return ticks%reportEveryTicks == 0
}

// poll connects to the mpv socket and queries time-pos + pause once per second.
func (p *Player) poll(stop <-chan struct{}, tr *tracker, onProgress func(Status)) {
	conn := p.dialWithRetry(stop)
	if conn == nil {
		return
	}
	defer conn.Close()

	reader := bufio.NewReader(conn)
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	ticks := 0
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			resp, err := getProperty(conn, reader, "time-pos")
			if err != nil {
				return // connection gone → mpv exited
			}
			if resp.Error == nil || *resp.Error != "success" {
				continue // no position yet (buffering) → skip
			}
			var pos float64
			if json.Unmarshal(resp.Data, &pos) != nil {
				continue
			}
			tr.set(pos) // track every second so the final position is accurate
			ticks++
			if onProgress != nil && shouldReport(ticks) {
				onProgress(Status{Position: pos, Paused: queryPaused(conn, reader)})
			}
		}
	}
}

// queryPaused queries the pause state; on any error it assumes false.
func queryPaused(conn net.Conn, reader *bufio.Reader) bool {
	resp, err := getProperty(conn, reader, "pause")
	if err != nil || resp.Error == nil || *resp.Error != "success" {
		return false
	}
	var paused bool
	_ = json.Unmarshal(resp.Data, &paused)
	return paused
}

// dialWithRetry waits until mpv has created the socket (up to ~5s).
func (p *Player) dialWithRetry(stop <-chan struct{}) net.Conn {
	for i := 0; i < 50; i++ {
		select {
		case <-stop:
			return nil
		default:
		}
		if conn, err := net.Dial("unix", p.socketPath); err == nil {
			return conn
		}
		time.Sleep(100 * time.Millisecond)
	}
	return nil
}
