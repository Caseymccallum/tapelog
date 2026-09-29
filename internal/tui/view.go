package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	styleTitle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("208"))
	styleDim   = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	styleSel   = lipgloss.NewStyle().Background(lipgloss.Color("236")).Bold(true)
	styleHelp  = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))

	kindStyle = map[Kind]lipgloss.Style{
		KindInfo:    lipgloss.NewStyle().Foreground(lipgloss.Color("245")),
		KindCall:    lipgloss.NewStyle().Foreground(lipgloss.Color("39")),
		KindResult:  lipgloss.NewStyle().Foreground(lipgloss.Color("250")),
		KindAllow:   lipgloss.NewStyle().Foreground(lipgloss.Color("42")),
		KindConfirm: lipgloss.NewStyle().Foreground(lipgloss.Color("214")),
		KindDeny:    lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true),
		KindError:   lipgloss.NewStyle().Foreground(lipgloss.Color("196")),
	}
)

// View renders the two-pane layout: event list + payload detail.
func (m model) View() string {
	if m.width == 0 {
		m.width, m.height = 100, 30
	}
	title := styleTitle.Render(fmt.Sprintf(" tapelog inspect — %s ", m.path))
	summary := styleDim.Render(fmt.Sprintf("%d events · %d calls · %d allowed · %d confirmed · %d denied · %d drift",
		m.sum.Events, m.sum.Calls, m.sum.Allowed, m.sum.Confirmed, m.sum.Denied, m.sum.Drift))
	header := title + "  " + summary

	listWidth := m.width * 55 / 100
	if listWidth < 40 {
		listWidth = 40
	}
	detailWidth := m.width - listWidth - 1
	if detailWidth < 30 {
		detailWidth = 30
	}

	left := m.renderList(listWidth, m.listHeight())
	right := m.renderDetail(detailWidth, m.listHeight())
	body := lipgloss.JoinHorizontal(lipgloss.Top, left, " "+styleDim.Render("│"), right)

	help := styleHelp.Render(" ↑/↓ move · tab pane · pgup/pgdn page · g/G ends · q quit")
	return header + "\n" + body + "\n" + help
}

// renderList draws the scrollable event list.
func (m model) renderList(width, height int) string {
	var b strings.Builder
	end := m.top + height
	if end > len(m.items) {
		end = len(m.items)
	}
	for i := m.top; i < end; i++ {
		it := m.items[i]
		marker := " "
		switch it.Kind {
		case KindDeny, KindError:
			marker = "!"
		case KindConfirm:
			marker = "?"
		case KindAllow:
			marker = "+"
		case KindCall:
			marker = ">"
		}
		line := fmt.Sprintf("%s %3d %-15s %s", marker, it.Seq, it.Type, it.Label)
		line = truncate(line, width)
		if i == m.cursor {
			b.WriteString(styleSel.Width(width).Render(line))
		} else {
			b.WriteString(kindStyle[it.Kind].Width(width).Render(line))
		}
		b.WriteString("\n")
	}
	return b.String()
}

// renderDetail draws the payload pane for the selected event.
func (m model) renderDetail(width, height int) string {
	if len(m.items) == 0 {
		return styleDim.Render("no events")
	}
	it := m.items[m.cursor]
	var lines []string
	lines = append(lines, styleTitle.Render(fmt.Sprintf("seq %d · %s", it.Seq, it.Type)))
	lines = append(lines, styleDim.Render(it.TS))
	lines = append(lines, kindStyle[it.Kind].Render(it.Label))
	lines = append(lines, "")
	lines = append(lines, strings.Split(it.Payload, "\n")...)

	if m.detail >= len(lines) {
		m.detail = len(lines) - 1
	}
	if m.detail < 0 {
		m.detail = 0
	}
	visible := lines[m.detail:]
	if len(visible) > height {
		visible = visible[:height]
	}
	var b strings.Builder
	for _, l := range visible {
		b.WriteString(truncate(l, width))
		b.WriteString("\n")
	}
	if len(lines) > height {
		b.WriteString(styleDim.Render(fmt.Sprintf("  … %d/%d lines (tab + ↑/↓)", m.detail+height, len(lines))))
	}
	return b.String()
}

// truncate shortens a line to the given display width.
func truncate(s string, width int) string {
	if width <= 1 {
		return s
	}
	r := []rune(s)
	if len(r) <= width {
		return s
	}
	return string(r[:width-1]) + "…"
}
