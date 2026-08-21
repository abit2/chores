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
	b.WriteString(muted.Render("Paste GitHub PR, Actions, or Jira URLs, or press enter to watch Slack Jira notifications.") + "\n\n")
	b.WriteString(m.input.View() + "\n")
	if m.status != "" {
		b.WriteString("\n" + bucketStyle("fail").Render(m.status) + "\n")
	}
	b.WriteString("\n" + help.Render("enter add  ·  esc quit") + "\n")
	return b.String()
}

func (m model) viewWatch() string {
	header := m.headerLine()
	body := m.body()
	if m.ready {
		body = m.viewport.View()
	}
	parts := []string{header, "", body}
	if m.status != "" && m.mode != modeAdd {
		parts = append(parts, "", lipgloss.NewStyle().Foreground(colors().pending).Render(truncate(m.status, max(1, m.width))))
	}
	if m.mode == modeAdd {
		parts = append(parts, "", m.viewAddPanel())
	} else {
		parts = append(parts, "", m.helpLine())
	}
	return strings.Join(parts, "\n")
}

func (m model) helpLine() string {
	_, _, help := m.styles()
	return help.Render(wrapHelp(helpItems(), max(1, m.width)))
}

func helpItems() []string {
	return []string{
		"a add",
		"d remove",
		"C clear list",
		"c clear notes",
		"N clear all notes",
		"j/k select",
		"enter expand",
		"o open",
		"r refresh",
		"R refresh all",
		"q quit",
	}
}

func wrapHelp(items []string, width int) string {
	sep := "  ·  "
	var lines []string
	var cur string
	for _, item := range items {
		next := item
		if cur != "" {
			next = cur + sep + item
		}
		if lipgloss.Width(next) <= width {
			cur = next
			continue
		}
		if cur != "" {
			lines = append(lines, cur)
		}
		if lipgloss.Width(item) > width {
			cur = truncate(item, width)
		} else {
			cur = item
		}
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	return strings.Join(lines, "\n")
}

func (m model) viewAddPanel() string {
	c := colors()
	title := lipgloss.NewStyle().Bold(true).Foreground(c.accent).Render("Add PRs, Actions runs, or Jira issues")
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
		return lipgloss.NewStyle().Foreground(colors().muted).Render("no pull requests, Jira issues, or Actions runs")
	}

	inner := m.cardWidth()
	var prs, tickets, runs []int
	for i, row := range m.rows {
		switch row.snap.Kind {
		case ghpr.KindRun:
			runs = append(runs, i)
		case ghpr.KindJira:
			tickets = append(tickets, i)
		default:
			prs = append(prs, i)
		}
	}

	var sections []string
	if len(prs) > 0 {
		sections = append(sections, m.renderSection("Pull requests", prs, inner))
	}
	if len(tickets) > 0 {
		sections = append(sections, m.renderSection("Jira", tickets, inner))
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
		border = c.selected
	case snap.Err != nil:
		border = c.fail
	case snap.Kind == ghpr.KindJira:
		border = bucketColor(jiraBucket(snap.State))
	case strings.EqualFold(snap.State, "MERGED"):
		border = c.merge
	case sum.Finished() && sum.Fail > 0:
		border = c.fail
	case sum.Finished() && sum.Passed():
		border = c.pass
	case sum.Pending > 0:
		border = c.pending
	}

	// width is lipgloss content width (padding included, border not).
	innerWidth := max(1, width-2)
	var lines []string
	lines = append(lines, m.cardTitle(snap, sum, row.notified, selected, innerWidth))
	if meta := cardMeta(snap, innerWidth); meta != "" {
		lines = append(lines, meta)
	}

	muted := lipgloss.NewStyle().Foreground(c.muted)
	if row.refreshing {
		lines = append(lines, m.spinner.View()+" "+muted.Render("refreshing"))
	}
	if snap.Err != nil {
		lines = append(lines, bucketStyle("fail").Render(truncate(snap.Err.Error(), innerWidth)))
	} else if snap.Kind == ghpr.KindJira {
		if snap.Title == "" && m.polling {
			spin := m.spinner.View() + " "
			msg := truncate("waiting for Slack notification", max(1, innerWidth-lipgloss.Width(spin)))
			lines = append(lines, spin+muted.Render(msg))
		} else if snap.Title == "" {
			lines = append(lines, muted.Render(truncate("waiting for Slack notification", innerWidth)))
		} else if row.expanded {
			lines = append(lines, m.renderJiraDetails(snap, innerWidth)...)
		} else {
			lines = append(lines, muted.Render("enter to expand"))
		}
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
		lines = append(lines, muted.Render(truncate(wait, innerWidth)))
	} else {
		barWidth := max(8, min(36, innerWidth))
		if bar := renderBar(sum, barWidth); bar != "" {
			lines = append(lines, bar)
		}
		lines = append(lines, muted.Render(truncate(counts(sum), innerWidth)))
		if row.expanded {
			lines = append(lines, m.renderChecks(snap.Checks, innerWidth)...)
		} else {
			lines = append(lines, muted.Render(
				truncate(fmt.Sprintf("%d checks hidden · enter to expand", sum.Total()), innerWidth)))
		}
	}

	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(border).
		Padding(0, 1).
		Width(width)
	if selected {
		style = style.
			BorderForeground(c.selected).
			Background(c.selectedBg)
	}
	return style.Render(strings.Join(lines, "\n"))
}

func (m model) cardTitle(snap ghpr.Snapshot, sum ghpr.Summary, notified, selected bool, width int) string {
	c := colors()
	id := snap.Input
	switch {
	case snap.Kind == ghpr.KindJira:
		id = snap.IssueKey
		if id == "" {
			id = "Jira"
		}
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
	badgeRendered := renderStatusBadge(snap, sum, notified)

	marker := "  "
	if selected {
		marker = lipgloss.NewStyle().Bold(true).Foreground(c.selected).Render("▶ ")
		sel := lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#1E1E2E")).
			Background(c.selected).
			Render(" SEL ")
		withSel := sel + " " + badgeRendered
		if lipgloss.Width(withSel)+6 < width {
			badgeRendered = withSel
		}
	}

	idStyled := lipgloss.NewStyle().Bold(true).Foreground(c.text).Render(id)
	left := marker + idStyled
	if repo != "" {
		withRepo := left + lipgloss.NewStyle().Foreground(c.muted).Render("  "+repo)
		if lipgloss.Width(withRepo)+1+lipgloss.Width(badgeRendered) <= width {
			left = withRepo
		}
	}
	gap := width - lipgloss.Width(left) - lipgloss.Width(badgeRendered)
	var top string
	switch {
	case gap >= 0:
		top = left + strings.Repeat(" ", gap) + badgeRendered
	case lipgloss.Width(marker)+lipgloss.Width(idStyled)+1+lipgloss.Width(badgeRendered) <= width:
		top = marker + idStyled + " " + badgeRendered
	default:
		top = marker + lipgloss.NewStyle().Bold(true).Foreground(c.text).Render(truncate(id, max(1, width-lipgloss.Width(marker))))
	}
	return top + "\n" + lipgloss.NewStyle().Foreground(c.text).Render("  "+truncate(title, max(1, width-2)))
}

func cardMeta(snap ghpr.Snapshot, width int) string {
	var parts []metaPart
	if snap.Kind == ghpr.KindJira {
		if snap.IssueType != "" {
			parts = append(parts, metaPart{snap.IssueType, "muted"})
		}
		if snap.Author != "" {
			parts = append(parts, metaPart{snap.Author, "muted"})
		}
		if rel := relativeJiraUpdated(snap.Updated); rel != "" {
			parts = append(parts, metaPart{rel, "muted"})
		}
		return renderMeta(parts, width)
	}
	if snap.Kind == ghpr.KindRun {
		if snap.Event != "" {
			parts = append(parts, metaPart{snap.Event, "muted"})
		}
		if snap.HeadRefName != "" {
			parts = append(parts, metaPart{snap.HeadRefName, "muted"})
		}
		if snap.RunID > 0 {
			parts = append(parts, metaPart{fmt.Sprintf("run %d", snap.RunID), "muted"})
		}
		if rel := relativeFetched(snap.FetchedAt); rel != "" {
			parts = append(parts, metaPart{rel, "muted"})
		}
		return renderMeta(parts, width)
	}
	if snap.Author != "" {
		parts = append(parts, metaPart{"@" + snap.Author, "muted"})
	}
	if snap.HeadRefName != "" {
		parts = append(parts, metaPart{snap.HeadRefName, "muted"})
	}
	if snap.State != "" && !strings.EqualFold(snap.State, "OPEN") {
		bucket := "skipping"
		text := strings.ToLower(snap.State)
		if strings.EqualFold(snap.State, "MERGED") {
			bucket = "merged"
			text = iconPRMerged + " merged"
		}
		parts = append(parts, metaPart{text, bucket})
	}
	if snap.UnresolvedComments > 0 {
		parts = append(parts, metaPart{
			fmt.Sprintf("%s %d unresolved", iconPRUnresolved, snap.UnresolvedComments),
			"pending",
		})
	}
	if rel := relativeFetched(snap.FetchedAt); rel != "" {
		parts = append(parts, metaPart{rel, "muted"})
	}
	return renderMeta(parts, width)
}

type metaPart struct {
	text   string
	bucket string
}

func renderMeta(parts []metaPart, width int) string {
	if len(parts) == 0 {
		return ""
	}
	plain := make([]string, len(parts))
	for i, p := range parts {
		plain[i] = p.text
	}
	joined := strings.Join(plain, " · ")
	if width > 0 && lipgloss.Width(joined) > width {
		return lipgloss.NewStyle().Foreground(colors().muted).Render(truncate(joined, width))
	}
	sep := lipgloss.NewStyle().Foreground(colors().muted).Render(" · ")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p.bucket == "muted" {
			out = append(out, lipgloss.NewStyle().Foreground(colors().muted).Render(p.text))
			continue
		}
		out = append(out, bucketStyle(p.bucket).Render(p.text))
	}
	return strings.Join(out, sep)
}

func relativeFetched(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Since(t)
	if d < 0 {
		d = 0
	}
	return "refreshed " + ghpr.FormatDuration(d) + " ago"
}

func (m model) renderJiraDetails(snap ghpr.Snapshot, width int) []string {
	muted := lipgloss.NewStyle().Foreground(colors().muted)
	var lines []string
	add := func(label, value string) {
		if value == "" {
			return
		}
		lines = append(lines, muted.Render(truncate(label+": "+value, width)))
	}
	add("status", snap.State)
	add("assignee", snap.Author)
	add("type", snap.IssueType)
	add("priority", snap.Priority)
	if rel := relativeJiraUpdated(snap.Updated); rel != "" {
		add("updated", rel)
	}
	add("last comment", snap.LastCommenter)
	if len(lines) == 0 {
		lines = append(lines, muted.Render("no details yet"))
	}
	return lines
}

func jiraBucket(state string) string {
	s := strings.ToLower(state)
	switch {
	case strings.Contains(s, "done"), strings.Contains(s, "closed"), strings.Contains(s, "resolved"), strings.Contains(s, "complete"):
		return "pass"
	case strings.Contains(s, "block"), strings.Contains(s, "fail"), strings.Contains(s, "won't"), strings.Contains(s, "cancel"):
		return "fail"
	case strings.Contains(s, "progress"), strings.Contains(s, "review"), strings.Contains(s, "testing"):
		return "pending"
	default:
		return "skipping"
	}
}

func bucketColor(bucket string) lipgloss.Color {
	c := colors()
	switch bucket {
	case "pass":
		return c.pass
	case "fail":
		return c.fail
	case "pending":
		return c.pending
	case "merged":
		return c.merge
	default:
		return c.border
	}
}

func relativeJiraUpdated(s string) string {
	t, ok := parseJiraTime(s)
	if !ok {
		return ""
	}
	d := time.Since(t)
	if d < 0 {
		d = 0
	}
	return ghpr.FormatDuration(d) + " ago"
}

func parseJiraTime(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	layouts := []string{
		"2006-01-02T15:04:05.999-0700",
		"2006-01-02T15:04:05.000-0700",
		time.RFC3339Nano,
		time.RFC3339,
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
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

const (
	iconPRMerged     = "⎇"
	iconPRUnresolved = "!"
	iconPRClosed     = "⊘"
)

type badgeSeg struct {
	text   string
	bucket string
}

func prBadgeSegs(snap ghpr.Snapshot, sum ghpr.Summary) []badgeSeg {
	var segs []badgeSeg
	switch strings.ToUpper(snap.State) {
	case "MERGED":
		segs = append(segs, badgeSeg{iconPRMerged + " merged", "merged"})
	case "CLOSED":
		segs = append(segs, badgeSeg{iconPRClosed + " closed", "skipping"})
	default:
		outcome := sum.Outcome()
		segs = append(segs, badgeSeg{outcome, bucketFromOutcome(outcome)})
	}
	if snap.UnresolvedComments > 0 {
		segs = append(segs, badgeSeg{
			fmt.Sprintf("%s %d unresolved", iconPRUnresolved, snap.UnresolvedComments),
			"pending",
		})
	}
	return segs
}

func prBadge(snap ghpr.Snapshot, sum ghpr.Summary) (badge, bucket string) {
	segs := prBadgeSegs(snap, sum)
	if len(segs) == 0 {
		badge = sum.Outcome()
		return badge, bucketFromOutcome(badge)
	}
	texts := make([]string, len(segs))
	for i, seg := range segs {
		texts[i] = seg.text
	}
	return strings.Join(texts, " · "), segs[0].bucket
}

func renderStatusBadge(snap ghpr.Snapshot, sum ghpr.Summary, notified bool) string {
	muted := lipgloss.NewStyle().Foreground(colors().muted)
	sep := muted.Render(" · ")
	var bits []string
	if snap.IsDraft {
		bits = append(bits, muted.Render("draft"))
	}
	switch snap.Kind {
	case ghpr.KindJira:
		badge := snap.State
		if badge == "" {
			badge = "loading"
		}
		bits = append(bits, bucketStyle(jiraBucket(snap.State)).Render(badge))
	case ghpr.KindRun:
		outcome := sum.Outcome()
		bits = append(bits, bucketStyle(bucketFromOutcome(outcome)).Render(outcome))
	default:
		for _, seg := range prBadgeSegs(snap, sum) {
			bits = append(bits, bucketStyle(seg.bucket).Render(seg.text))
		}
	}
	if notified {
		bits = append(bits, muted.Render("pinged"))
	}
	return strings.Join(bits, sep)
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
