package ui

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/abit2/chores/internal/ghpr"
	"github.com/abit2/chores/internal/notify"
	"github.com/abit2/chores/internal/store"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const fetchTimeout = 30 * time.Second

// Config is the watcher settings from the CLI.
type Config struct {
	Refs     []string
	Repo     string
	Hidden   []string
	Interval time.Duration
	Required bool
	NoSound  bool
	ExitDone bool
}

type mode int

const (
	modeInput mode = iota
	modeWatch
	modeAdd
)

type prRow struct {
	snap     ghpr.Snapshot
	notified bool
	expanded bool
	pinned   bool
}

type model struct {
	cfg      Config
	mode     mode
	input    textarea.Model
	spinner  spinner.Model
	viewport viewport.Model
	ready    bool

	rows     []prRow
	selected int
	hidden   []string
	polling  bool
	pollSeq  int
	lastPoll time.Time
	status   string
	width    int
	height   int
}

type pollMsg struct {
	seq         int
	snaps       []ghpr.Snapshot
	discovered  []ghpr.Snapshot
	discoverErr error
}

type tickMsg time.Time

type notifiedMsg struct {
	index int
}

// New returns the Bubble Tea model for the CI watcher.
func New(cfg Config) tea.Model {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("#FF75B5"))

	ta := textarea.New()
	ta.Placeholder = "https://github.com/org/repo/pull/123  or  /actions/runs/456"
	ta.SetWidth(72)
	ta.SetHeight(6)
	ta.ShowLineNumbers = false
	ta.CharLimit = 4000
	ta.Focus()

	m := model{
		cfg:     cfg,
		hidden:  append([]string(nil), cfg.Hidden...),
		spinner: sp,
		input:   ta,
		width:   80,
		height:  24,
	}
	if len(cfg.Refs) > 0 || cfg.Repo != "" {
		m.mode = modeWatch
		m.rows = rowsFromRefs(cfg.Refs)
		m.polling = true
		m.pollSeq = 1
		_ = m.persist()
	} else {
		m.mode = modeInput
	}
	return m
}

func rowsFromRefs(refs []string) []prRow {
	rows := make([]prRow, len(refs))
	for i, ref := range refs {
		rows[i] = prRow{
			snap:     seedSnap(ref),
			expanded: true,
			pinned:   true,
		}
	}
	return rows
}

func seedSnap(ref string) ghpr.Snapshot {
	if ghpr.IsRunRef(ref) {
		return ghpr.Snapshot{
			Kind:  ghpr.KindRun,
			Input: ref,
			Repo:  ghpr.RepoFromRef(ref),
			RunID: ghpr.RunIDFromRef(ref),
		}
	}
	return ghpr.Snapshot{Kind: ghpr.KindPR, Input: ref, Repo: ghpr.RepoFromRef(ref)}
}

func (m model) Init() tea.Cmd {
	if m.mode == modeWatch {
		return tea.Batch(m.spinner.Tick, m.poll(), tick(m.cfg.Interval), tea.SetWindowTitle("CI Watcher"))
	}
	return tea.Batch(textarea.Blink, tea.SetWindowTitle("CI Watcher"))
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.layout()
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		if m.ready && m.watching() {
			m.viewport.SetContent(m.body())
		}
		return m, cmd

	case tea.KeyMsg:
		switch m.mode {
		case modeInput:
			return m.updateInput(msg)
		case modeAdd:
			return m.updateAdd(msg)
		default:
			return m.updateWatch(msg)
		}

	case tickMsg:
		if !m.watching() || m.polling {
			return m, tick(m.cfg.Interval)
		}
		return m, tea.Batch(m.startPoll(), tick(m.cfg.Interval))

	case pollMsg:
		return m.applyPoll(msg)

	case notifiedMsg:
		if msg.index >= 0 && msg.index < len(m.rows) {
			m.rows[msg.index].notified = true
		}
		return m, nil
	}

	if m.mode == modeInput || m.mode == modeAdd {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m model) updateInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "esc":
		return m, tea.Quit
	case "ctrl+s", "ctrl+d", "enter":
		return m.submitInput(true)
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m model) updateAdd(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.mode = modeWatch
		m.status = ""
		m.input.Blur()
		m.layout()
		return m, nil
	case "ctrl+s", "ctrl+d", "enter":
		return m.submitInput(false)
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m model) submitInput(startTicker bool) (tea.Model, tea.Cmd) {
	refs := ghpr.ParseRefs([]string{m.input.Value()})
	if len(refs) == 0 {
		m.status = "paste at least one PR URL"
		return m, nil
	}
	added, skipped := m.appendRefs(refs)
	if added == 0 {
		if skipped > 0 {
			m.status = "already watching " + pluralize(skipped, "that PR", "those PRs")
		} else {
			m.status = "paste at least one PR URL"
		}
		return m, nil
	}
	m.mode = modeWatch
	m.status = addStatus(added, skipped)
	m.input.Reset()
	m.input.Blur()
	if m.selected < 0 || m.selected >= len(m.rows) {
		m.selected = 0
	}
	m.layout()
	if err := m.persist(); err != nil {
		m.status = err.Error()
	}
	cmds := []tea.Cmd{m.startPoll(), tea.SetWindowTitle(m.watchingTitle())}
	if startTicker {
		cmds = append(cmds, m.spinner.Tick, tick(m.cfg.Interval))
	}
	return m, tea.Batch(cmds...)
}

func (m *model) appendRefs(refs []string) (added, skipped int) {
	firstNew := -1
	for _, ref := range refs {
		if m.hasRef(ref) {
			skipped++
			continue
		}
		m.rows = append(m.rows, prRow{
			snap:     seedSnap(ref),
			expanded: true,
			pinned:   true,
		})
		m.unhide(ref)
		m.cfg.Refs = append(m.cfg.Refs, ref)
		if firstNew < 0 {
			firstNew = len(m.rows) - 1
		}
		added++
	}
	if firstNew >= 0 {
		m.selected = firstNew
	}
	return added, skipped
}

func (m model) hasRef(ref string) bool {
	probe := seedSnap(ref)
	for _, row := range m.rows {
		if ghpr.Same(row.snap, probe) {
			return true
		}
	}
	return false
}

func (m model) updateWatch(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "j", "down":
		m.status = ""
		m.moveSelection(1)
		m.viewport.SetContent(m.body())
		return m, nil
	case "k", "up":
		m.status = ""
		m.moveSelection(-1)
		m.viewport.SetContent(m.body())
		return m, nil
	case "enter", " ":
		if len(m.rows) > 0 {
			m.rows[m.selected].expanded = !m.rows[m.selected].expanded
			m.viewport.SetContent(m.body())
		}
		return m, nil
	case "d", "x", "delete", "backspace":
		m.removeSelected()
		m.viewport.SetContent(m.body())
		return m, nil
	case "C":
		m.clearSaved()
		m.viewport.SetContent(m.body())
		return m, nil
	case "r":
		if m.polling {
			return m, nil
		}
		return m, m.startPoll()
	case "a", "+":
		m.mode = modeAdd
		m.status = ""
		m.input.Reset()
		m.input.SetHeight(3)
		m.input.Placeholder = "https://github.com/org/repo/pull/123  or  /actions/runs/456"
		m.input.Focus()
		m.layout()
		return m, textarea.Blink
	case "o":
		if len(m.rows) == 0 {
			return m, nil
		}
		return m, openItem(m.rows[m.selected].snap)
	}
	return m, nil
}

func (m *model) moveSelection(delta int) {
	order := m.displayOrder()
	if len(order) == 0 {
		return
	}
	pos := 0
	for i, idx := range order {
		if idx == m.selected {
			pos = i
			break
		}
	}
	pos += delta
	if pos < 0 {
		pos = 0
	}
	if pos >= len(order) {
		pos = len(order) - 1
	}
	m.selected = order[pos]
}

func (m model) displayOrder() []int {
	prs := make([]int, 0, len(m.rows))
	runs := make([]int, 0, len(m.rows))
	for i, row := range m.rows {
		if row.snap.Kind == ghpr.KindRun {
			runs = append(runs, i)
		} else {
			prs = append(prs, i)
		}
	}
	return append(prs, runs...)
}

func (m *model) removeSelected() {
	if len(m.rows) == 0 {
		return
	}
	i := m.selected
	if i < 0 || i >= len(m.rows) {
		return
	}
	row := m.rows[i]
	url := persistURL(row.snap)
	label := url
	if row.snap.Kind == ghpr.KindRun && row.snap.WorkflowName != "" {
		label = row.snap.WorkflowName
	} else if row.snap.Number > 0 {
		label = fmt.Sprintf("#%d", row.snap.Number)
	}
	if row.pinned {
		m.cfg.Refs = dropMatching(m.cfg.Refs, row.snap)
	} else if url != "" {
		m.hidden = store.MergeURLs(m.hidden, []string{url})
	}
	m.rows = append(m.rows[:i], m.rows[i+1:]...)
	if m.selected >= len(m.rows) {
		m.selected = max(0, len(m.rows)-1)
	}
	m.status = "removed " + label
	if err := m.persist(); err != nil {
		m.status = err.Error()
	}
}

func (m *model) clearSaved() {
	n := 0
	for _, row := range m.rows {
		if row.pinned {
			n++
		}
	}
	if n == 0 && len(m.hidden) == 0 && len(m.rows) == 0 {
		m.status = "nothing to clear"
		return
	}
	m.rows = nil
	m.cfg.Refs = nil
	m.hidden = nil
	m.selected = 0
	if err := m.persist(); err != nil {
		m.status = err.Error()
		return
	}
	switch {
	case n == 0:
		m.status = "cleared watch list"
	case n == 1:
		m.status = "cleared 1 saved URL"
	default:
		m.status = fmt.Sprintf("cleared %d saved URLs", n)
	}
}

func persistURL(snap ghpr.Snapshot) string {
	if snap.URL != "" {
		return snap.URL
	}
	return snap.Input
}

func (m *model) persist() error {
	urls := make([]string, 0, len(m.rows))
	for _, row := range m.rows {
		if !row.pinned {
			continue
		}
		if u := persistURL(row.snap); u != "" {
			urls = append(urls, u)
		}
	}
	return store.Save(store.Watch{
		URLs:   urls,
		Repo:   m.cfg.Repo,
		Hidden: m.hidden,
	})
}

func (m *model) unhide(ref string) {
	probe := seedSnap(ref)
	kept := m.hidden[:0]
	if kept == nil {
		kept = []string{}
	}
	for _, h := range m.hidden {
		if ghpr.Same(seedSnap(h), probe) {
			continue
		}
		kept = append(kept, h)
	}
	m.hidden = kept
}

func (m model) isHidden(snap ghpr.Snapshot) bool {
	return hiddenHas(m.hidden, snap)
}

func hiddenHas(hidden []string, snap ghpr.Snapshot) bool {
	for _, h := range hidden {
		if ghpr.Same(seedSnap(h), snap) {
			return true
		}
	}
	return false
}

func dropMatching(refs []string, snap ghpr.Snapshot) []string {
	out := make([]string, 0, len(refs))
	for _, ref := range refs {
		if ghpr.Same(seedSnap(ref), snap) {
			continue
		}
		out = append(out, ref)
	}
	return out
}

func (m model) applyPoll(msg pollMsg) (tea.Model, tea.Cmd) {
	latest := msg.seq == m.pollSeq
	if latest {
		m.polling = false
		m.lastPoll = time.Now()
		if msg.discoverErr != nil {
			m.status = msg.discoverErr.Error()
		}
	}

	var cmds []tea.Cmd
	for _, snap := range msg.discovered {
		if m.indexOf(snap) >= 0 || m.isHidden(snap) {
			continue
		}
		m.rows = append(m.rows, prRow{snap: snap, expanded: true})
	}
	for _, snap := range msg.snaps {
		i := m.indexOf(snap)
		if i < 0 {
			continue
		}
		prev := m.rows[i]
		wasFinished := ghpr.Summarize(prev.snap.Checks).Finished()
		nowFinished := ghpr.Summarize(snap.Checks).Finished()
		m.rows[i].snap = snap
		if nowFinished && !wasFinished && !prev.notified {
			cmds = append(cmds, pingCmd(i, snap, !m.cfg.NoSound))
		}
		if !nowFinished {
			m.rows[i].notified = false
		}
	}
	if m.selected >= len(m.rows) {
		m.selected = max(0, len(m.rows)-1)
	}
	if m.ready {
		m.viewport.SetContent(m.body())
	}
	if latest {
		if err := m.persist(); err != nil && m.status == "" {
			m.status = err.Error()
		}
	}
	cmds = append(cmds, tea.SetWindowTitle(m.watchingTitle()))

	if latest && m.cfg.ExitDone && len(m.rows) > 0 {
		allDone := true
		for _, row := range m.rows {
			if row.snap.Err != nil || !ghpr.Summarize(row.snap.Checks).Finished() {
				allDone = false
				break
			}
		}
		if allDone {
			cmds = append(cmds, tea.Quit)
		}
	}
	return m, tea.Batch(cmds...)
}

func (m model) indexOf(snap ghpr.Snapshot) int {
	for i, row := range m.rows {
		if ghpr.Same(row.snap, snap) {
			return i
		}
	}
	return -1
}

func (m *model) startPoll() tea.Cmd {
	m.pollSeq++
	m.polling = true
	return m.poll()
}

func (m model) poll() tea.Cmd {
	seq := m.pollSeq
	rows := append([]prRow(nil), m.rows...)
	required := m.cfg.Required
	watchRepo := m.cfg.Repo
	hidden := append([]string(nil), m.hidden...)
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
		defer cancel()

		var discovered []ghpr.Snapshot
		var discoverErr error
		if watchRepo != "" {
			listed, err := ghpr.ListRuns(ctx, watchRepo, 15)
			if err != nil {
				discoverErr = err
			} else {
				known := make([]ghpr.Snapshot, 0, len(rows))
				for _, row := range rows {
					known = append(known, row.snap)
				}
				for _, snap := range listed {
					if indexOfSnaps(known, snap) >= 0 || hiddenHas(hidden, snap) {
						continue
					}
					if !ghpr.Summarize(snap.Checks).Finished() {
						detailed := ghpr.Refresh(ctx, snap, required)
						if detailed.Err == nil {
							snap = detailed
						}
					}
					discovered = append(discovered, snap)
					known = append(known, snap)
				}
			}
		}

		snaps := make([]ghpr.Snapshot, len(rows))
		var wg sync.WaitGroup
		for i, row := range rows {
			wg.Add(1)
			go func(i int, row prRow) {
				defer wg.Done()
				itemCtx, itemCancel := context.WithTimeout(context.Background(), fetchTimeout)
				defer itemCancel()
				snaps[i] = ghpr.Refresh(itemCtx, row.snap, required)
			}(i, row)
		}
		wg.Wait()
		return pollMsg{seq: seq, snaps: snaps, discovered: discovered, discoverErr: discoverErr}
	}
}

func indexOfSnaps(snaps []ghpr.Snapshot, snap ghpr.Snapshot) int {
	for i, s := range snaps {
		if ghpr.Same(s, snap) {
			return i
		}
	}
	return -1
}

func pingCmd(index int, snap ghpr.Snapshot, sound bool) tea.Cmd {
	return func() tea.Msg {
		sum := ghpr.Summarize(snap.Checks)
		title := fmt.Sprintf("CI %s", sum.Outcome())
		switch {
		case snap.Kind == ghpr.KindRun:
			name := snap.WorkflowName
			if name == "" {
				name = "Actions"
			}
			title = fmt.Sprintf("Actions %s · %s", sum.Outcome(), name)
		case snap.Number > 0:
			title = fmt.Sprintf("CI %s · PR #%d", sum.Outcome(), snap.Number)
		}
		body := snap.Title
		if body == "" {
			body = snap.Input
		}
		if snap.Repo != "" {
			body = snap.Repo + " — " + body
		}
		notify.Alert(title, body, sound)
		return notifiedMsg{index: index}
	}
}

func openItem(snap ghpr.Snapshot) tea.Cmd {
	return func() tea.Msg {
		switch {
		case snap.Kind == ghpr.KindRun && snap.RunID > 0:
			args := []string{"run", "view", fmt.Sprintf("%d", snap.RunID), "--web"}
			if snap.Repo != "" {
				args = append(args, "--repo", snap.Repo)
			}
			_ = exec.Command("gh", args...).Start()
		default:
			ref := snap.URL
			if ref == "" {
				ref = snap.Input
			}
			_ = exec.Command("gh", "pr", "view", ref, "--web").Start()
		}
		return nil
	}
}

// ExitSummary is a plain-text recap printed after the TUI quits.
func ExitSummary(tm tea.Model) string {
	m, ok := tm.(model)
	if !ok || len(m.rows) == 0 {
		return ""
	}
	var b strings.Builder
	for _, row := range m.rows {
		snap := row.snap
		label := snap.Input
		switch {
		case snap.Kind == ghpr.KindRun:
			label = snap.WorkflowName
			if label == "" {
				label = "actions"
			}
			if snap.Repo != "" {
				label = snap.Repo + " " + label
			}
		case snap.Number > 0:
			label = fmt.Sprintf("#%d", snap.Number)
			if snap.Repo != "" {
				label = snap.Repo + " " + label
			}
		}
		switch {
		case snap.Err != nil:
			fmt.Fprintf(&b, "%s  error  %s\n", label, snap.Err)
		default:
			sum := ghpr.Summarize(snap.Checks)
			fmt.Fprintf(&b, "%s  %s  %s\n", label, sum.Outcome(), counts(sum))
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func tick(d time.Duration) tea.Cmd {
	if d <= 0 {
		d = 10 * time.Second
	}
	return tea.Tick(d, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m model) watchingTitle() string {
	pending := 0
	for _, row := range m.rows {
		if !ghpr.Summarize(row.snap.Checks).Finished() {
			pending++
		}
	}
	if pending == 0 && m.lastPoll.IsZero() {
		return "CI Watcher"
	}
	if pending == 0 {
		return "CI Watcher · done"
	}
	return fmt.Sprintf("CI Watcher · %d pending", pending)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func (m model) headerLine() string {
	title, muted, _ := m.styles()
	left := title.Render("CI Watcher")
	if m.polling {
		left = m.spinner.View() + " " + left
	} else {
		left = "  " + left
	}

	prs, runs := 0, 0
	for _, row := range m.rows {
		if row.snap.Kind == ghpr.KindRun {
			runs++
		} else {
			prs++
		}
	}
	var bits []string
	switch {
	case prs > 0 && runs > 0:
		bits = append(bits, fmt.Sprintf("%d PR%s · %d action%s", prs, plural(prs), runs, plural(runs)))
	case runs > 0:
		bits = append(bits, fmt.Sprintf("%d action%s", runs, plural(runs)))
	default:
		bits = append(bits, fmt.Sprintf("%d PR%s", prs, plural(prs)))
	}
	if m.cfg.Repo != "" {
		bits = append(bits, m.cfg.Repo)
	}
	bits = append(bits, fmt.Sprintf("every %s", m.cfg.Interval))
	if m.cfg.Required {
		bits = append(bits, "required only")
	}
	if !m.lastPoll.IsZero() {
		bits = append(bits, "refreshed "+ghpr.FormatDuration(time.Since(m.lastPoll))+" ago")
	} else {
		bits = append(bits, "fetching…")
	}
	right := muted.Render(strings.Join(bits, " · "))
	gap := max(1, m.width-lipgloss.Width(left)-lipgloss.Width(right)-2)
	return left + strings.Repeat(" ", gap) + right
}

func (m model) watching() bool {
	return m.mode == modeWatch || m.mode == modeAdd
}

func (m *model) layout() {
	if m.width == 0 {
		return
	}
	chrome := 6
	if m.mode == modeAdd {
		chrome = 14
	}
	m.input.SetWidth(max(20, m.width-8))
	vw := max(20, m.width-2)
	vh := max(3, m.height-chrome)
	if !m.ready {
		m.viewport = viewport.New(vw, vh)
		m.ready = true
	} else {
		m.viewport.Width = vw
		m.viewport.Height = vh
	}
	m.viewport.SetContent(m.body())
}

func addStatus(added, skipped int) string {
	s := fmt.Sprintf("added %d item%s", added, plural(added))
	if skipped > 0 {
		s += fmt.Sprintf(" · skipped %d duplicate%s", skipped, plural(skipped))
	}
	return s
}

func pluralize(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
