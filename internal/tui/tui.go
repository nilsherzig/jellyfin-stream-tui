package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/nilsherzig/jellyfin-stream-tui/internal/jellyfin"
)

// Backend is the slice of the Jellyfin API the TUI needs (mockable in tests).
type Backend interface {
	Views() ([]jellyfin.Item, error)
	Children(parentID string) ([]jellyfin.Item, error)
	Resume() ([]jellyfin.Item, error)
}

// PlayFunc plays an item and returns a tea.Cmd that emits a PlayDoneMsg when
// playback ends. This keeps the playback logic outside the TUI and testable.
type PlayFunc func(jellyfin.Item) tea.Cmd

// level is one navigation level (list + cursor + its ParentId).
type level struct {
	title    string
	parentID string // "" = top level (Views)
	items    []jellyfin.Item
	cursor   int
	// resumeCount counts the leading "Continue Watching" items (home page only).
	// The remaining items are the libraries.
	resumeCount int
}

// Model is the Bubble Tea model.
type Model struct {
	backend Backend
	play    PlayFunc

	cur    level
	stack  []level // breadcrumb of parent levels
	status string
	err    error
	quit   bool
}

// Message types for the update loop.
type itemsMsg struct {
	title       string
	parentID    string
	items       []jellyfin.Item
	resumeCount int
}
type errMsg struct{ err error }

// PlayDoneMsg is sent by the play function when mpv has exited.
type PlayDoneMsg struct{ Err error }

// New creates a Model.
func New(backend Backend, play PlayFunc) Model {
	return Model{backend: backend, play: play}
}

// Init loads the home page on startup.
func (m Model) Init() tea.Cmd {
	return m.loadRoot()
}

// loadRoot loads the home page: "Continue Watching" (Resume) followed by the
// libraries (Views). Both go into one list; resumeCount marks the boundary for
// the visual split.
func (m Model) loadRoot() tea.Cmd {
	return func() tea.Msg {
		views, err := m.backend.Views()
		if err != nil {
			return errMsg{err}
		}
		// A Resume error is not fatal — it just means no "Continue Watching" list.
		resume, _ := m.backend.Resume()
		items := append(resume, views...)
		return itemsMsg{title: "Home", parentID: "", items: items, resumeCount: len(resume)}
	}
}

// loadChildren loads a folder's children asynchronously.
func (m Model) loadChildren(parentID, title string) tea.Cmd {
	return func() tea.Msg {
		items, err := m.backend.Children(parentID)
		if err != nil {
			return errMsg{err}
		}
		return itemsMsg{title: title, parentID: parentID, items: items}
	}
}

// selected returns the currently highlighted item (or nil).
func (m Model) selected() *jellyfin.Item {
	if m.cur.cursor < 0 || m.cur.cursor >= len(m.cur.items) {
		return nil
	}
	return &m.cur.items[m.cur.cursor]
}

// Update handles messages.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.KeyMsg:
		return m.handleKey(msg)

	case itemsMsg:
		m.err = nil
		m.cur = level{title: msg.title, parentID: msg.parentID, items: msg.items, resumeCount: msg.resumeCount}
		return m, nil

	case errMsg:
		m.err = msg.err
		return m, nil

	case PlayDoneMsg:
		if msg.Err != nil {
			m.status = "Playback error: " + msg.Err.Error()
		} else {
			m.status = "Playback finished"
		}
		// Reload the current level so the resume position updates.
		if m.cur.parentID == "" {
			return m, m.loadRoot()
		}
		return m, m.loadChildren(m.cur.parentID, m.cur.title)
	}

	return m, nil
}

// handleKey handles keyboard input.
func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		m.quit = true
		return m, tea.Quit

	case "up", "k":
		if m.cur.cursor > 0 {
			m.cur.cursor--
		}
		return m, nil

	case "down", "j":
		if m.cur.cursor < len(m.cur.items)-1 {
			m.cur.cursor++
		}
		return m, nil

	case "enter", "l", "right":
		it := m.selected()
		if it == nil {
			return m, nil
		}
		if it.IsFolder {
			// Keep the current list visible until the new one loads (no "loading"
			// placeholder → no flicker). The user waits a moment.
			m.stack = append(m.stack, m.cur)
			m.status = ""
			return m, m.loadChildren(it.ID, it.Name)
		}
		if m.play == nil {
			return m, nil
		}
		m.status = "Playing: " + it.Name
		return m, m.play(*it)

	case "esc", "h", "left", "backspace":
		if len(m.stack) > 0 {
			m.cur = m.stack[len(m.stack)-1]
			m.stack = m.stack[:len(m.stack)-1]
		}
		return m, nil
	}
	return m, nil
}

// --- Rendering ---

var (
	titleStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	sectionStyle  = lipgloss.NewStyle().Bold(true).Underline(true).Foreground(lipgloss.Color("14"))
	cursorStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("13")).Bold(true)
	dimStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	progressStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
)

// View renders the current screen.
func (m Model) View() string {
	if m.quit {
		return ""
	}

	var b strings.Builder

	// Breadcrumb / title
	crumbs := make([]string, 0, len(m.stack)+1)
	for _, l := range m.stack {
		crumbs = append(crumbs, l.title)
	}
	crumbs = append(crumbs, m.cur.title)
	b.WriteString(titleStyle.Render(strings.Join(crumbs, " › ")))
	b.WriteString("\n\n")

	if m.err != nil {
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Render("Error: "+m.err.Error()) + "\n")
	}

	if len(m.cur.items) == 0 {
		b.WriteString(dimStyle.Render("(empty)") + "\n")
	}

	for i, it := range m.cur.items {
		// On the home page: section headers that split "Continue Watching" from
		// the libraries.
		if m.cur.resumeCount > 0 {
			if i == 0 {
				b.WriteString(sectionStyle.Render("Continue Watching") + "\n")
			} else if i == m.cur.resumeCount {
				b.WriteString("\n" + sectionStyle.Render("Libraries") + "\n")
			}
		}

		cursor := "  "
		line := it.DisplayName()
		if i == m.cur.cursor {
			cursor = cursorStyle.Render("▶ ")
			line = cursorStyle.Render(line)
		}
		b.WriteString(cursor + line + " " + badge(it) + "\n")
	}

	b.WriteString("\n")
	if m.status != "" {
		b.WriteString(progressStyle.Render(m.status) + "\n")
	}
	b.WriteString(dimStyle.Render("↑/↓ move · ⏎ open/play · esc back · q quit"))
	return b.String()
}

// badge shows folder/playback state after an item.
func badge(it jellyfin.Item) string {
	if it.IsFolder {
		return dimStyle.Render("›")
	}
	if it.UserData.Played {
		return progressStyle.Render("[✓]")
	}
	// Resume percentage, if playback has started.
	if it.RunTimeTicks > 0 && it.UserData.PlaybackPositionTicks > 0 {
		pct := int(float64(it.UserData.PlaybackPositionTicks) / float64(it.RunTimeTicks) * 100)
		return progressStyle.Render(fmt.Sprintf("[%d%%]", pct))
	}
	return ""
}
