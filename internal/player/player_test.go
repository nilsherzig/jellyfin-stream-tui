package player

import (
	"os/exec"
	"strings"
	"testing"
	"time"
)

// hasArg reports whether an argument appears exactly.
func hasArg(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}

// Positive: mpvArgs contains the socket, no-terminal, and the URL.
func TestMpvArgs_Basic(t *testing.T) {
	p := New("/tmp/sock")
	args := p.mpvArgs("http://stream", 0)

	if !hasArg(args, "--input-ipc-server=/tmp/sock") {
		t.Errorf("missing IPC socket: %v", args)
	}
	if !hasArg(args, "--no-terminal") {
		t.Errorf("missing --no-terminal (else mpv fights the TUI for the terminal): %v", args)
	}
	if args[len(args)-1] != "http://stream" {
		t.Errorf("stream URL must be the last argument: %v", args)
	}
	// Without a resume position there must be no --start.
	for _, a := range args {
		if strings.HasPrefix(a, "--start=") {
			t.Errorf("did not expect --start at position 0: %v", args)
		}
	}
}

// Positive: extra mpv flags from JFTUI_MPV_ARGS are appended.
func TestMpvArgs_ExtraFromEnv(t *testing.T) {
	t.Setenv("JFTUI_MPV_ARGS", "--vo=null --no-video")
	p := New("/tmp/sock")
	args := p.mpvArgs("http://stream", 0)
	if !hasArg(args, "--vo=null") || !hasArg(args, "--no-video") {
		t.Errorf("missing extra flags: %v", args)
	}
}

// Positive: a resume position sets --start.
func TestMpvArgs_Resume(t *testing.T) {
	p := New("/tmp/sock")
	args := p.mpvArgs("http://stream", 42.7)
	if !hasArg(args, "--start=42") {
		t.Errorf("expected --start=42, got: %v", args)
	}
}

// Positive: a valid time-pos reply from mpv parses.
func TestParseTimePos_Success(t *testing.T) {
	pos, err := parseTimePos([]byte(`{"data":123.5,"error":"success"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pos != 123.5 {
		t.Fatalf("pos = %v, want 123.5", pos)
	}
}

// Negative: an error reply (e.g. property unavailable) returns an error.
func TestParseTimePos_Error(t *testing.T) {
	if _, err := parseTimePos([]byte(`{"data":null,"error":"property unavailable"}`)); err == nil {
		t.Fatal("expected error for error!=success, got nil")
	}
}

// Negative: non-JSON (e.g. an mpv event line) returns an error.
func TestParseTimePos_NotJSON(t *testing.T) {
	if _, err := parseTimePos([]byte(`broken`)); err == nil {
		t.Fatal("expected error for broken JSON, got nil")
	}
}

// Positive: Stop kills the running process so the TUI exit closes the stream.
func TestStop_KillsProcess(t *testing.T) {
	p := New("/tmp/sock")
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	p.mu.Lock()
	p.cmd = cmd
	p.mu.Unlock()

	p.Stop()

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected the killed process to return an error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("process was not killed within 5s")
	}
}

// Negative: Stop while idle (no process) must not panic.
func TestStop_NoProcess(t *testing.T) {
	New("/tmp/sock").Stop()
}
