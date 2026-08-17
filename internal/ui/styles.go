package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/personal/chores/internal/ghpr"
)

type palette struct {
	accent  lipgloss.Color
	muted   lipgloss.Color
	pass    lipgloss.Color
	fail    lipgloss.Color
	pending lipgloss.Color
	skip    lipgloss.Color
	text    lipgloss.Color
	border  lipgloss.Color
}

func colors() palette {
	return palette{
		accent:  lipgloss.Color("#FF75B5"),
		muted:   lipgloss.Color("#6C7086"),
		pass:    lipgloss.Color("#A6E3A1"),
		fail:    lipgloss.Color("#F38BA8"),
		pending: lipgloss.Color("#F9E2AF"),
		skip:    lipgloss.Color("#7F849C"),
		text:    lipgloss.Color("#CDD6F4"),
		border:  lipgloss.Color("#45475A"),
	}
}

func (m model) styles() (title, muted, help lipgloss.Style) {
	c := colors()
	title = lipgloss.NewStyle().Bold(true).Foreground(c.accent)
	muted = lipgloss.NewStyle().Foreground(c.muted)
	help = lipgloss.NewStyle().Foreground(c.muted)
	return
}

func bucketStyle(bucket string) lipgloss.Style {
	c := colors()
	switch strings.ToLower(bucket) {
	case "pass":
		return lipgloss.NewStyle().Foreground(c.pass)
	case "fail":
		return lipgloss.NewStyle().Foreground(c.fail)
	case "pending":
		return lipgloss.NewStyle().Foreground(c.pending)
	case "skipping", "cancel":
		return lipgloss.NewStyle().Foreground(c.skip)
	default:
		return lipgloss.NewStyle().Foreground(c.text)
	}
}

func bucketIcon(bucket string) string {
	switch strings.ToLower(bucket) {
	case "pass":
		return "✓"
	case "fail":
		return "✗"
	case "pending":
		return "●"
	case "cancel":
		return "⊘"
	default:
		return "–"
	}
}

func renderBar(s ghpr.Summary, width int) string {
	c := colors()
	total := s.Total()
	if total == 0 || width < 8 {
		return ""
	}
	type seg struct {
		n     int
		color lipgloss.Color
	}
	segs := []seg{
		{s.Pass, c.pass},
		{s.Fail, c.fail},
		{s.Pending, c.pending},
		{s.Skip + s.Cancel, c.skip},
	}
	cells := make([]int, len(segs))
	used := 0
	for i, seg := range segs {
		if seg.n == 0 {
			continue
		}
		n := int(float64(seg.n) / float64(total) * float64(width))
		if n < 1 {
			n = 1
		}
		cells[i] = n
		used += n
	}
	for used > width {
		for i := range cells {
			if cells[i] > 1 && used > width {
				cells[i]--
				used--
			}
		}
	}
	var b strings.Builder
	for i, seg := range segs {
		if cells[i] == 0 {
			continue
		}
		b.WriteString(lipgloss.NewStyle().Foreground(seg.color).Render(strings.Repeat("█", cells[i])))
	}
	if used < width {
		b.WriteString(lipgloss.NewStyle().Foreground(c.border).Render(strings.Repeat("░", width-used)))
	}
	return b.String()
}

func truncate(s string, width int) string {
	if width <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= width {
		return s
	}
	if width <= 1 {
		return string(runes[:width])
	}
	return string(runes[:width-1]) + "…"
}

func counts(s ghpr.Summary) string {
	parts := []string{
		fmt.Sprintf("%d pass", s.Pass),
		fmt.Sprintf("%d pending", s.Pending),
		fmt.Sprintf("%d fail", s.Fail),
	}
	if s.Skip > 0 {
		parts = append(parts, fmt.Sprintf("%d skip", s.Skip))
	}
	if s.Cancel > 0 {
		parts = append(parts, fmt.Sprintf("%d cancel", s.Cancel))
	}
	return strings.Join(parts, " · ")
}
