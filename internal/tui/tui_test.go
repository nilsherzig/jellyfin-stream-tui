package tui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/nilsherzig/jellyfin-stream-tui/internal/jellyfin"
)

// fakeBackend returns fixed answers for tests.
type fakeBackend struct {
	children []jellyfin.Item
	err      error
	gotPaPID string
}

func (f *fakeBackend) Views() ([]jellyfin.Item, error)  { return f.children, f.err }
func (f *fakeBackend) Resume() ([]jellyfin.Item, error) { return nil, nil }
func (f *fakeBackend) Children(parentID string) ([]jellyfin.Item, error) {
	f.gotPaPID = parentID
	return f.children, f.err
}

// key builds a key message from a string.
func key(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

// enter returns the Enter key message (Bubble Tea uses KeyEnter, not "\r").
func enter() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyEnter} }

// newTestModel builds a Model with a prefilled item list.
func newTestModel(items []jellyfin.Item) Model {
	m := New(&fakeBackend{}, nil)
	m.cur = level{title: "Test", items: items}
	return m
}

var sampleItems = []jellyfin.Item{
	{ID: "1", Name: "Movie A", Type: "Movie"},
	{ID: "2", Name: "Folder B", IsFolder: true},
	{ID: "3", Name: "Movie C", Type: "Movie"},
}

// Positive: "j" moves the cursor down.
func TestCursorDown(t *testing.T) {
	m := newTestModel(sampleItems)
	updated, _ := m.Update(key("j"))
	if updated.(Model).cur.cursor != 1 {
		t.Fatalf("cursor = %d, want 1", updated.(Model).cur.cursor)
	}
}

// Negative: "k" at the top must not go below 0.
func TestCursorUp_BoundedAtZero(t *testing.T) {
	m := newTestModel(sampleItems)
	updated, _ := m.Update(key("k"))
	if updated.(Model).cur.cursor != 0 {
		t.Fatalf("cursor = %d, want 0 (bound)", updated.(Model).cur.cursor)
	}
}

// Negative: "j" at the bottom must not go past the last item.
func TestCursorDown_BoundedAtEnd(t *testing.T) {
	m := newTestModel(sampleItems)
	m.cur.cursor = 2
	updated, _ := m.Update(key("j"))
	if updated.(Model).cur.cursor != 2 {
		t.Fatalf("cursor = %d, want 2 (bound)", updated.(Model).cur.cursor)
	}
}

// Positive: Enter on a folder pushes the current level and loads.
func TestEnterFolder_PushesStack(t *testing.T) {
	m := newTestModel(sampleItems)
	m.cur.cursor = 1 // "Folder B"
	updated, cmd := m.Update(enter())
	um := updated.(Model)
	if len(um.stack) != 1 {
		t.Fatalf("stack length = %d, want 1", len(um.stack))
	}
	if cmd == nil {
		t.Fatal("expected a load command, got nil")
	}
}

// Positive: Enter on a playable item calls the play function.
func TestEnterItem_Plays(t *testing.T) {
	var played jellyfin.Item
	m := New(&fakeBackend{}, func(it jellyfin.Item) tea.Cmd {
		played = it
		return func() tea.Msg { return nil }
	})
	m.cur = level{title: "T", items: sampleItems, cursor: 0} // "Movie A"
	if _, cmd := m.Update(enter()); cmd == nil {
		t.Fatal("expected a play command, got nil")
	}
	if played.ID != "1" {
		t.Fatalf("played wrong item: %q", played.ID)
	}
}

// Negative: Back (esc) on an empty stack leaves the level unchanged.
func TestBack_EmptyStack(t *testing.T) {
	m := newTestModel(sampleItems)
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if updated.(Model).cur.title != "Test" || len(updated.(Model).stack) != 0 {
		t.Fatal("nothing should change on an empty stack")
	}
}

// Positive: itemsMsg sets the current level.
func TestItemsMsg_SetsLevel(t *testing.T) {
	m := newTestModel(nil)
	updated, _ := m.Update(itemsMsg{title: "New", items: sampleItems})
	um := updated.(Model)
	if um.cur.title != "New" || len(um.cur.items) != 3 {
		t.Fatalf("level not set: %+v", um.cur)
	}
}

// Positive: with resumeCount>0 the View splits "Continue Watching" from "Libraries".
func TestView_ResumeSection(t *testing.T) {
	m := newTestModel(sampleItems)
	m.cur.resumeCount = 1 // first item is Continue Watching, rest are libraries
	out := m.View()
	if !strings.Contains(out, "Continue Watching") || !strings.Contains(out, "Libraries") {
		t.Fatalf("missing section headers:\n%s", out)
	}
}

// Negative: without resume items (resumeCount==0) there are no headers.
func TestView_NoResumeSection(t *testing.T) {
	m := newTestModel(sampleItems)
	out := m.View()
	if strings.Contains(out, "Continue Watching") {
		t.Fatalf("did not expect a 'Continue Watching' header:\n%s", out)
	}
}

// Negative: errMsg sets the error state.
func TestErrMsg(t *testing.T) {
	m := newTestModel(nil)
	updated, _ := m.Update(errMsg{err: errors.New("boom")})
	if updated.(Model).err == nil {
		t.Fatal("err should be set")
	}
}
