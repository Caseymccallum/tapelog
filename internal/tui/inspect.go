// Package tui implements cassette's interactive session viewer.
package tui

import (
	tea "github.com/charmbracelet/bubbletea"
)

// Inspect runs the interactive session viewer over a loaded log.
func Inspect(path string) error {
	items, sum, err := Load(path)
	if err != nil {
		return err
	}
	m := newModel(path, items, sum)
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err = p.Run()
	return err
}

type model struct {
	path        string
	items       []Item
	sum         Summary
	cursor      int // selected item
	top         int // first visible item
	detail      int // detail-pane scroll
	focusDetail bool
	width       int
	height      int
}

func newModel(path string, items []Item, sum Summary) model {
	return model{path: path, items: items, sum: sum}
}

func (m model) Init() tea.Cmd { return nil }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			return m, tea.Quit
		case "tab":
			m.focusDetail = !m.focusDetail
		case "j", "down":
			if m.focusDetail {
				m.detail++
			} else if m.cursor < len(m.items)-1 {
				m.cursor++
				m.detail = 0
			}
		case "k", "up":
			if m.focusDetail {
				if m.detail > 0 {
					m.detail--
				}
			} else if m.cursor > 0 {
				m.cursor--
				m.detail = 0
			}
		case "pgdown", "right":
			m.cursor += m.listHeight()
			m.detail = 0
		case "pgup", "left":
			m.cursor -= m.listHeight()
			m.detail = 0
		case "g", "home":
			m.cursor, m.detail = 0, 0
		case "G", "end":
			m.cursor = len(m.items) - 1
			m.detail = 0
		}
	}
	m.clamp()
	return m, nil
}

// listHeight is the number of list rows that fit on screen.
func (m model) listHeight() int {
	h := m.height - 4 // title + summary + help + padding
	if h < 3 {
		h = 3
	}
	return h
}

// clamp keeps the cursor and the scroll window in range.
func (m *model) clamp() {
	if len(m.items) == 0 {
		m.cursor, m.top = 0, 0
		return
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor > len(m.items)-1 {
		m.cursor = len(m.items) - 1
	}
	h := m.listHeight()
	if m.cursor < m.top {
		m.top = m.cursor
	}
	if m.cursor >= m.top+h {
		m.top = m.cursor - h + 1
	}
	if m.top < 0 {
		m.top = 0
	}
	if m.detail < 0 {
		m.detail = 0
	}
}
