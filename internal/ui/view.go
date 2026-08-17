package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/personal/chores/internal/ghpr"
)

func (m model) View() string {
	if m.width == 0 {
		return "loading…"
	}
	if m.mode == modeInput {
		return m.viewInput()
	}
	return m.viewWatch()
}

func (m model) viewInput() string {
	title, muted, help := m.styles()
	var b strings.Builder
	b.WriteString(title.Render("CI Watcher") + "\n")
	b.WriteString(muted.Render("Paste GitHub PR URLs, then enter to start watching.") + "\n\n")
	b.WriteString(m.input.View() + "\n")
	if m.status != "" {
		b.WriteString("\n" + bucketStyle("fail").Render(m.status) + "\n")
	}
	b.WriteString("\n" + help.Render("enter add  ·  esc quit") + "\n")
	return b.String()
}

func (m model) viewWatch() string {
	_, _, help := m.styles()
	header := m.headerLine()
	body := m.body()
	if m.ready {
		body = m.viewport.View()
	}
	parts := []string{header, "", body}
	if m.status != "" && m.mode != modeAdd {
		parts = append(parts, "", lipgloss.NewStyle().Foreground(colors().pending).Render(m.status))
	}
	if m.mode == modeAdd {
		parts = append(parts, "", m.viewAddPanel())
	} else {
		parts = append(parts, help.Render("a add  ·  j/k select  ·  enter checks  ·  o browser  ·  r refresh  ·  q quit"))
	}
	return strings.Join(parts, "\n")
}

func (m model) viewAddPanel() string {
	c := colors()
	title := lipgloss.NewStyle().Bold(true).Foreground(c.accent).Render("Add pull requests")
	hint := lipgloss.NewStyle().Foreground(c.muted).Render("enter / ctrl+s add  ·  esc cancel")
	var body strings.Builder
	body.WriteString(title + "\n")
	body.WriteString(m.input.View())
	if m.status != "" {
		body.WriteString("\n" + bucketStyle("fail").Render(m.status))
	}
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(c.accent).
		Padding(0, 1).
		Width(max(20, m.width-2))
	return box.Render(body.String()) + "\n" + hint
}

func (m model) body() string {
	if len(m.rows) == 0 {
		return "no pull requests"
	}
	cards := make([]string, 0, len(m.rows))
	inner := max(20, m.width-4)
	for i, row := range m.rows {
		cards = append(cards, m.renderCard(i, row, inner))
	}
	return lipgloss.JoinVertical(lipgloss.Left, cards...)
}

func (m model) renderCard(i int, row prRow, width int) string {
	c := colors()
	snap := row.snap
	sum := ghpr.Summarize(snap.Checks)
	border := c.border
	switch {
	case i == m.selected:
		border = c.accent
	case snap.Err != nil:
		border = c.fail
	case sum.Finished() && sum.Fail > 0:
		border = c.fail
	case sum.Finished() && sum.Passed():
		border = c.pass
	case sum.Pending > 0:
		border = c.pending
	}

	var lines []string
	lines = append(lines, m.cardTitle(snap, sum, row.notified, width-4))
	if meta := cardMeta(snap); meta != "" {
		lines = append(lines, lipgloss.NewStyle().Foreground(c.muted).Render(truncate(meta, width-4)))
	}

	if snap.Err != nil {
		lines = append(lines, bucketStyle("fail").Render(truncate(snap.Err.Error(), width-4)))
	} else if sum.Total() == 0 {
		wait := "waiting for checks to start"
		if m.polling && snap.Title == "" {
			wait = m.spinner.View() + " loading pull request"
		}
		lines = append(lines, lipgloss.NewStyle().Foreground(c.muted).Render(wait))
	} else {
		barWidth := max(10, min(36, width-8))
		bar := renderBar(sum, barWidth)
		lines = append(lines, bar+"  "+lipgloss.NewStyle().Foreground(c.muted).Render(counts(sum)))
		if row.expanded {
			lines = append(lines, m.renderChecks(snap.Checks, width-4)...)
		} else {
			lines = append(lines, lipgloss.NewStyle().Foreground(c.muted).Render(
				fmt.Sprintf("%d checks hidden · enter to expand", sum.Total())))
		}
	}

	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(border).
		Padding(0, 1).
		Width(width)
	return style.Render(strings.Join(lines, "\n"))
}

func (m model) cardTitle(snap ghpr.Snapshot, sum ghpr.Summary, notified bool, width int) string {
	c := colors()
	id := snap.Input
	if snap.Number > 0 {
		id = fmt.Sprintf("#%d", snap.Number)
	}
	repo := snap.Repo
	title := snap.Title
	if title == "" {
		title = "loading…"
	}
	badge := sum.Outcome()
	badgeStyle := bucketStyle(bucketFromOutcome(badge))
	if snap.IsDraft {
		badge = "draft · " + badge
	}
	if notified {
		badge += " · pinged"
	}

	left := lipgloss.NewStyle().Bold(true).Foreground(c.text).Render(id)
	if repo != "" {
		left += lipgloss.NewStyle().Foreground(c.muted).Render("  " + repo)
	}
	right := badgeStyle.Render(badge)
	gap := max(1, width-lipgloss.Width(left)-lipgloss.Width(right))
	top := left + strings.Repeat(" ", gap) + right
	return top + "\n" + lipgloss.NewStyle().Foreground(c.text).Render(truncate(title, width))
}

func cardMeta(snap ghpr.Snapshot) string {
	var parts []string
	if snap.Author != "" {
		parts = append(parts, "@"+snap.Author)
	}
	if snap.HeadRefName != "" {
		parts = append(parts, snap.HeadRefName)
	}
	if snap.State != "" && !strings.EqualFold(snap.State, "OPEN") {
		parts = append(parts, strings.ToLower(snap.State))
	}
	return strings.Join(parts, " · ")
}

func (m model) renderChecks(checks []ghpr.Check, width int) []string {
	now := time.Now()
	nameWidth := max(16, width-18)
	out := make([]string, 0, len(checks))
	for _, check := range checks {
		icon := bucketStyle(check.Bucket).Render(bucketIcon(check.Bucket))
		name := check.Name
		if check.Workflow != "" && !strings.HasPrefix(name, check.Workflow) {
			name = check.Workflow + " / " + name
		}
		dur := check.Duration(now)
		durS := ""
		if dur != "" {
			durS = lipgloss.NewStyle().Foreground(colors().muted).Render(dur)
		}
		line := fmt.Sprintf("%s  %-*s  %s", icon, nameWidth, truncate(name, nameWidth), durS)
		out = append(out, strings.TrimRight(line, " "))
	}
	return out
}

func bucketFromOutcome(outcome string) string {
	switch outcome {
	case "passed":
		return "pass"
	case "failed":
		return "fail"
	case "running", "waiting for checks":
		return "pending"
	default:
		return "skipping"
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
