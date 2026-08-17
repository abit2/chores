package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/abit2/chores/internal/ghpr"
	"github.com/charmbracelet/lipgloss"
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
	b.WriteString(muted.Render("Paste GitHub PR or Actions run URLs, then enter to start watching.") + "\n\n")
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
		parts = append(parts, help.Render("a add  ·  d remove  ·  C clear  ·  j/k select  ·  enter checks  ·  o browser  ·  r refresh  ·  q quit"))
	}
	return strings.Join(parts, "\n")
}

func (m model) viewAddPanel() string {
	c := colors()
	title := lipgloss.NewStyle().Bold(true).Foreground(c.accent).Render("Add PRs or Actions runs")
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
		if m.cfg.Repo != "" {
			return lipgloss.NewStyle().Foreground(colors().muted).Render("no Actions runs yet")
		}
		return lipgloss.NewStyle().Foreground(colors().muted).Render("no pull requests or Actions runs")
	}

	inner := max(20, m.width-4)
	var prs, runs []int
	for i, row := range m.rows {
		if row.snap.Kind == ghpr.KindRun {
			runs = append(runs, i)
		} else {
			prs = append(prs, i)
		}
	}

	var sections []string
	if len(prs) > 0 {
		sections = append(sections, m.renderSection("Pull requests", prs, inner))
	}
	if len(runs) > 0 || m.cfg.Repo != "" {
		sections = append(sections, m.renderSection("Actions", runs, inner))
	}
	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

func (m model) renderSection(title string, indexes []int, width int) string {
	c := colors()
	head := lipgloss.NewStyle().Bold(true).Foreground(c.accent).Render(title)
	head += lipgloss.NewStyle().Foreground(c.muted).Render(fmt.Sprintf("  ·  %d", len(indexes)))
	rule := lipgloss.NewStyle().Foreground(c.border).Render(strings.Repeat("─", max(8, width)))
	parts := []string{head, rule}
	if len(indexes) == 0 {
		parts = append(parts, lipgloss.NewStyle().Foreground(c.muted).Render("none"))
	} else {
		cards := make([]string, 0, len(indexes))
		for _, i := range indexes {
			cards = append(cards, m.renderCard(i, m.rows[i], width))
		}
		parts = append(parts, lipgloss.JoinVertical(lipgloss.Left, cards...))
	}
	return strings.Join(parts, "\n")
}

func (m model) renderCard(i int, row prRow, width int) string {
	c := colors()
	snap := row.snap
	sum := ghpr.Summarize(snap.Checks)
	selected := i == m.selected
	border := c.border
	switch {
	case selected:
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

	innerWidth := width - 4
	if selected {
		innerWidth -= 2
	}
	var lines []string
	lines = append(lines, m.cardTitle(snap, sum, row.notified, selected, innerWidth))
	if meta := cardMeta(snap); meta != "" {
		lines = append(lines, lipgloss.NewStyle().Foreground(c.muted).Render(truncate(meta, innerWidth)))
	}

	if snap.Err != nil {
		lines = append(lines, bucketStyle("fail").Render(truncate(snap.Err.Error(), width-4)))
	} else if sum.Total() == 0 {
		wait := "waiting for checks to start"
		if m.polling && snap.Title == "" {
			wait = m.spinner.View() + " loading"
			if snap.Kind == ghpr.KindRun {
				wait += " Actions run"
			} else {
				wait += " pull request"
			}
		}
		lines = append(lines, lipgloss.NewStyle().Foreground(c.muted).Render(wait))
	} else {
		barWidth := max(10, min(36, width-8))
		bar := renderBar(sum, barWidth)
		lines = append(lines, bar+"  "+lipgloss.NewStyle().Foreground(c.muted).Render(counts(sum)))
		if row.expanded {
			lines = append(lines, m.renderChecks(snap.Checks, innerWidth)...)
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
	if selected {
		style = style.
			Border(lipgloss.ThickBorder()).
			BorderForeground(c.accent).
			Background(c.selectedBg).
			Padding(0, 1)
	}
	return style.Render(strings.Join(lines, "\n"))
}

func (m model) cardTitle(snap ghpr.Snapshot, sum ghpr.Summary, notified, selected bool, width int) string {
	c := colors()
	id := snap.Input
	switch {
	case snap.Kind == ghpr.KindRun:
		id = snap.WorkflowName
		if id == "" {
			id = "Actions"
		}
	case snap.Number > 0:
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

	marker := "  "
	if selected {
		marker = lipgloss.NewStyle().Bold(true).Foreground(c.accent).Render("▶ ")
		sel := lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#1E1E2E")).
			Background(c.accent).
			Render(" SELECTED ")
		badge = sel + " " + badgeStyle.Render(badge)
	} else {
		badge = badgeStyle.Render(badge)
	}

	left := marker + lipgloss.NewStyle().Bold(true).Foreground(c.text).Render(id)
	if repo != "" {
		left += lipgloss.NewStyle().Foreground(c.muted).Render("  " + repo)
	}
	gap := max(1, width-lipgloss.Width(left)-lipgloss.Width(badge))
	top := left + strings.Repeat(" ", gap) + badge
	return top + "\n" + lipgloss.NewStyle().Foreground(c.text).Render("  "+truncate(title, max(1, width-2)))
}

func cardMeta(snap ghpr.Snapshot) string {
	var parts []string
	if snap.Kind == ghpr.KindRun {
		if snap.Event != "" {
			parts = append(parts, snap.Event)
		}
		if snap.HeadRefName != "" {
			parts = append(parts, snap.HeadRefName)
		}
		if snap.RunID > 0 {
			parts = append(parts, fmt.Sprintf("run %d", snap.RunID))
		}
		return strings.Join(parts, " · ")
	}
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
