package ui

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/abit2/chores/internal/ghpr"
	"github.com/abit2/chores/internal/jira"
	"github.com/abit2/chores/internal/notify"
	"github.com/abit2/chores/internal/store"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const fetchTimeout = 60 * time.Second

// DefaultInterval is how often chores polls when -i / --interval is omitted.
const DefaultInterval = 10 * time.Minute

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
	snap       ghpr.Snapshot
	notified   bool
	expanded   bool
	pinned     bool
	refreshing bool
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
	seq           int
	snaps         []ghpr.Snapshot
	discovered    []ghpr.Snapshot
	discoverErr   error
	githubFetched bool
}

type pollKind int

const (
	pollAll pollKind = iota
	pollDue
	pollCached
)

type itemMsg struct {
	prev ghpr.Snapshot
	snap ghpr.Snapshot
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
	ta.Placeholder = "PR, Actions, or Jira URL — Jira is filled from Slack notifications"
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
		m.hydrateRows()
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
	if jira.IsRef(ref) {
		parsed, _ := jira.Parse(ref)
		return ghpr.Snapshot{
			Kind:     ghpr.KindJira,
			Input:    ref,
			Repo:     parsed.Host,
			IssueKey: parsed.Key,
			URL:      parsed.BrowseURL(),
		}
	}
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
		kind := pollAll
		if m.hasGitHubCache() {
			kind = pollCached
		}
		return tea.Batch(m.spinner.Tick, m.poll(kind), tick(m.cfg.Interval), tea.SetWindowTitle("CI Watcher"))
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
			m.showBody()
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
		return m, tea.Batch(m.beginPoll(pollDue), tick(m.cfg.Interval))

	case pollMsg:
		return m.applyPoll(msg)

	case itemMsg:
		return m.applyItem(msg)

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
		if startTicker {
			m.mode = modeWatch
			m.status = "watching Slack Jira notifications"
			m.input.Reset()
			m.input.Blur()
			m.layout()
			return m, tea.Batch(m.startPoll(), tea.SetWindowTitle(m.watchingTitle()), m.spinner.Tick, tick(m.cfg.Interval))
		}
		m.status = "paste at least one PR, Actions, or Jira URL"
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
		m.hydrateRow(len(m.rows) - 1)
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
		m.showBody()
		return m, nil
	case "k", "up":
		m.status = ""
		m.moveSelection(-1)
		m.showBody()
		return m, nil
	case "enter", " ":
		if len(m.rows) > 0 {
			m.rows[m.selected].expanded = !m.rows[m.selected].expanded
			m.showBody()
		}
		return m, nil
	case "d", "x", "delete", "backspace":
		m.removeSelected()
		m.showBody()
		return m, nil
	case "C":
		m.clearSaved()
		m.showBody()
		return m, nil
	case "c":
		m.clearSelectedNotes()
		m.showBody()
		return m, nil
	case "N":
		m.clearAllNotes()
		m.showBody()
		return m, nil
	case "r":
		if len(m.rows) == 0 {
			return m, nil
		}
		return m, m.refreshSelected()
	case "R":
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
	tickets := make([]int, 0, len(m.rows))
	runs := make([]int, 0, len(m.rows))
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
	order := append(prs, tickets...)
	return append(order, runs...)
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
	switch {
	case row.snap.Kind == ghpr.KindRun && row.snap.WorkflowName != "":
		label = row.snap.WorkflowName
	case row.snap.Kind == ghpr.KindJira && row.snap.IssueKey != "":
		label = row.snap.IssueKey
	case row.snap.Number > 0:
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

func (m *model) clearSelectedNotes() {
	if len(m.rows) == 0 {
		m.status = "nothing to clear"
		return
	}
	i := m.selected
	if i < 0 || i >= len(m.rows) {
		return
	}
	key := jiraIssueKey(m.rows[i].snap)
	if key == "" {
		m.status = "select a Jira issue"
		return
	}
	if err := jira.ClearKey(key); err != nil {
		m.status = err.Error()
		return
	}
	resetJiraRow(&m.rows[i])
	m.status = "cleared notifications for " + key
}

func (m *model) clearAllNotes() {
	n := 0
	for i := range m.rows {
		if jiraIssueKey(m.rows[i].snap) != "" || m.rows[i].snap.Kind == ghpr.KindJira {
			n++
			resetJiraRow(&m.rows[i])
		}
	}
	if err := jira.ClearAll(); err != nil {
		m.status = err.Error()
		return
	}
	if n == 0 {
		m.status = "cleared all Jira notifications"
		return
	}
	m.status = fmt.Sprintf("cleared notifications for %d Jira %s", n, pluralize(n, "issue", "issues"))
}

func resetJiraRow(row *prRow) {
	row.notified = false
	row.refreshing = false
	snap := row.snap
	snap.Title = ""
	snap.LastCommenter = ""
	snap.Updated = ""
	snap.State = "waiting for Slack"
	snap.Err = nil
	row.snap = snap
}

func jiraIssueKey(snap ghpr.Snapshot) string {
	if snap.IssueKey != "" {
		return snap.IssueKey
	}
	if ref, ok := jira.Parse(snap.URL); ok {
		return ref.Key
	}
	if ref, ok := jira.Parse(snap.Input); ok {
		return ref.Key
	}
	return ""
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
		URLs:     urls,
		Repo:     m.cfg.Repo,
		Hidden:   m.hidden,
		LastPoll: m.lastPoll,
	})
}

func (m *model) persistPRs() error {
	snaps := make([]ghpr.Snapshot, 0, len(m.rows))
	for _, row := range m.rows {
		if row.snap.Kind == ghpr.KindJira {
			continue
		}
		if row.snap.FetchedAt.IsZero() && row.snap.Number == 0 && row.snap.RunID == 0 && len(row.snap.Checks) == 0 {
			continue
		}
		snaps = append(snaps, row.snap)
	}
	return store.SavePRs(snaps)
}

func (m *model) persistAll() error {
	if err := m.persist(); err != nil {
		return err
	}
	return m.persistPRs()
}

func (m *model) hydrateRows() {
	cache, err := store.LoadPRs()
	if err == nil && len(cache) > 0 {
		for i := range m.rows {
			m.applyCache(i, cache)
		}
	}
	if w, err := store.Load(); err == nil && !w.LastPoll.IsZero() {
		m.lastPoll = w.LastPoll
	} else {
		var latest time.Time
		for _, row := range m.rows {
			if t := row.snap.FetchedAt; t.After(latest) {
				latest = t
			}
		}
		m.lastPoll = latest
	}
}

func (m *model) hydrateRow(i int) {
	cache, err := store.LoadPRs()
	if err != nil || len(cache) == 0 {
		return
	}
	m.applyCache(i, cache)
}

func (m *model) applyCache(i int, cache map[string]store.PRRecord) {
	if i < 0 || i >= len(m.rows) {
		return
	}
	rec, ok := store.LookupPR(cache, m.rows[i].snap)
	if !ok {
		return
	}
	cached := rec.Snapshot()
	if cached.Input == "" {
		cached.Input = m.rows[i].snap.Input
	}
	m.rows[i].snap = cached
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
	firstPoll := m.lastPoll.IsZero()
	latest := msg.seq == m.pollSeq
	if latest {
		m.polling = false
		if msg.githubFetched {
			m.lastPoll = time.Now()
		}
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
		if !firstPoll && snap.Kind == ghpr.KindJira {
			cmds = append(cmds, pingCmd(len(m.rows)-1, snap, !m.cfg.NoSound))
		}
	}
	for _, snap := range msg.snaps {
		i := m.indexOf(snap)
		if i < 0 {
			continue
		}
		prev := m.rows[i]
		m.rows[i].snap = snap
		if snap.Kind == ghpr.KindJira {
			first := prev.snap.Updated == "" && prev.snap.Title == ""
			changed := snap.Updated != "" && snap.Updated != prev.snap.Updated
			if changed && !first {
				cmds = append(cmds, pingCmd(i, snap, !m.cfg.NoSound))
			}
			continue
		}
		wasFinished := ghpr.Summarize(prev.snap.Checks).Finished()
		nowFinished := ghpr.Summarize(snap.Checks).Finished()
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
		m.showBody()
	}
	if latest {
		if err := m.persistAll(); err != nil && m.status == "" {
			m.status = err.Error()
		}
	}
	cmds = append(cmds, tea.SetWindowTitle(m.watchingTitle()))

	if latest && m.cfg.ExitDone && len(m.rows) > 0 {
		ciRows := 0
		allDone := true
		for _, row := range m.rows {
			if row.snap.Kind == ghpr.KindJira {
				continue
			}
			ciRows++
			if row.snap.Err != nil || !ghpr.Summarize(row.snap.Checks).Finished() {
				allDone = false
				break
			}
		}
		if allDone && ciRows > 0 {
			cmds = append(cmds, tea.Quit)
		}
	}
	return m, tea.Batch(cmds...)
}

func (m *model) refreshSelected() tea.Cmd {
	if len(m.rows) == 0 {
		return nil
	}
	i := m.selected
	if i < 0 || i >= len(m.rows) {
		return nil
	}
	if m.rows[i].refreshing {
		return nil
	}
	m.rows[i].refreshing = true
	prev := m.rows[i].snap
	m.status = "refreshing " + itemLabel(prev)
	if m.ready {
		m.showBody()
	}
	required := m.cfg.Required
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
		defer cancel()
		ref := persistURL(prev)
		if ref == "" {
			ref = prev.Input
		}
		return itemMsg{prev: prev, snap: ghpr.Fetch(ctx, ref, required)}
	}
}

func (m model) applyItem(msg itemMsg) (tea.Model, tea.Cmd) {
	i := m.indexOf(msg.prev)
	if i < 0 {
		i = m.indexOf(msg.snap)
	}
	if i < 0 {
		return m, nil
	}
	m.rows[i].refreshing = false
	prev := m.rows[i]
	snap := keepOnError(msg.snap, prev.snap)
	m.rows[i].snap = snap
	if snap.Err != nil {
		m.status = snap.Err.Error()
	} else {
		m.status = itemLabel(snap) + " refreshed"
		if snap.Kind != ghpr.KindJira {
			m.lastPoll = snap.FetchedAt
			if m.lastPoll.IsZero() {
				m.lastPoll = time.Now()
			}
		}
	}
	var cmds []tea.Cmd
	if snap.Kind == ghpr.KindJira {
		first := prev.snap.Updated == "" && prev.snap.Title == ""
		changed := snap.Updated != "" && snap.Updated != prev.snap.Updated
		if changed && !first {
			cmds = append(cmds, pingCmd(i, snap, !m.cfg.NoSound))
		}
	} else {
		wasFinished := ghpr.Summarize(prev.snap.Checks).Finished()
		nowFinished := ghpr.Summarize(snap.Checks).Finished()
		if nowFinished && !wasFinished && !prev.notified {
			cmds = append(cmds, pingCmd(i, snap, !m.cfg.NoSound))
		}
		if !nowFinished {
			m.rows[i].notified = false
		}
	}
	if err := m.persistAll(); err != nil && m.status == "" {
		m.status = err.Error()
	}
	if m.ready {
		m.showBody()
	}
	cmds = append(cmds, tea.SetWindowTitle(m.watchingTitle()))
	return m, tea.Batch(cmds...)
}

func keepOnError(next, prev ghpr.Snapshot) ghpr.Snapshot {
	if next.Err == nil {
		return next
	}
	if next.Title == "" {
		next.Title = prev.Title
	}
	if next.Number == 0 {
		next.Number = prev.Number
	}
	if next.RunID == 0 {
		next.RunID = prev.RunID
	}
	if next.URL == "" {
		next.URL = prev.URL
	}
	if next.Repo == "" {
		next.Repo = prev.Repo
	}
	if next.Author == "" {
		next.Author = prev.Author
	}
	if next.HeadRefName == "" {
		next.HeadRefName = prev.HeadRefName
	}
	if next.WorkflowName == "" {
		next.WorkflowName = prev.WorkflowName
	}
	if len(next.Checks) == 0 {
		next.Checks = prev.Checks
	}
	if next.FetchedAt.IsZero() {
		next.FetchedAt = time.Now()
	}
	return next
}

func itemLabel(snap ghpr.Snapshot) string {
	switch {
	case snap.Kind == ghpr.KindJira && snap.IssueKey != "":
		return snap.IssueKey
	case snap.Kind == ghpr.KindRun && snap.WorkflowName != "":
		return snap.WorkflowName
	case snap.Number > 0:
		return fmt.Sprintf("#%d", snap.Number)
	}
	if u := persistURL(snap); u != "" {
		return u
	}
	return "item"
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
	return m.beginPoll(pollAll)
}

func (m *model) beginPoll(kind pollKind) tea.Cmd {
	m.pollSeq++
	m.polling = true
	return m.poll(kind)
}

func (m model) poll(kind pollKind) tea.Cmd {
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
		known := make([]ghpr.Snapshot, 0, len(rows))
		for _, row := range rows {
			known = append(known, row.snap)
		}
		if watchRepo != "" {
			listed, err := ghpr.ListRuns(ctx, watchRepo, 15)
			if err != nil {
				discoverErr = err
			} else {
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
		jiraListed, jiraErr := ghpr.ListJiraSlack(ctx, known)
		if jiraErr != nil && discoverErr == nil {
			discoverErr = jiraErr
		} else if jiraErr == nil {
			for _, snap := range jiraListed {
				if indexOfSnaps(known, snap) >= 0 || hiddenHas(hidden, snap) {
					continue
				}
				discovered = append(discovered, snap)
				known = append(known, snap)
			}
		}

		prev := make([]ghpr.Snapshot, len(rows))
		for i, row := range rows {
			prev[i] = row.snap
		}
		if kind != pollCached {
			ghpr.FillPRViews(ctx, prev)
		}
		snaps := make([]ghpr.Snapshot, len(prev))
		var wg sync.WaitGroup
		githubFetched := false
		for i, snap := range prev {
			if skipGitHubPoll(kind, snap) {
				snaps[i] = snap
				continue
			}
			if snap.Kind != ghpr.KindJira {
				githubFetched = true
			}
			wg.Add(1)
			go func(i int, snap ghpr.Snapshot) {
				defer wg.Done()
				itemCtx, itemCancel := context.WithTimeout(context.Background(), fetchTimeout)
				defer itemCancel()
				snaps[i] = ghpr.Refresh(itemCtx, snap, required)
			}(i, snap)
		}
		wg.Wait()
		return pollMsg{seq: seq, snaps: snaps, discovered: discovered, discoverErr: discoverErr, githubFetched: githubFetched}
	}
}

func skipGitHubPoll(kind pollKind, snap ghpr.Snapshot) bool {
	if snap.Kind == ghpr.KindJira {
		return false
	}
	switch kind {
	case pollAll:
		return false
	case pollDue:
		return ghpr.SkipGitHubPoll(snap)
	case pollCached:
		return !snap.FetchedAt.IsZero()
	default:
		return false
	}
}

func (m model) hasGitHubCache() bool {
	for _, row := range m.rows {
		if row.snap.Kind != ghpr.KindJira && !row.snap.FetchedAt.IsZero() {
			return true
		}
	}
	return false
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
		case snap.Kind == ghpr.KindJira:
			title = snap.IssueKey
			if title == "" {
				title = "Jira"
			}
			if snap.State != "" {
				title += " · " + snap.State
			}
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
		case snap.Kind == ghpr.KindJira:
			ref := snap.URL
			if ref == "" {
				ref = snap.Input
			}
			openURL(ref)
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

func openURL(u string) {
	if u == "" || strings.HasPrefix(u, "slack:") {
		return
	}
	switch runtime.GOOS {
	case "darwin":
		_ = exec.Command("open", u).Start()
	case "windows":
		_ = exec.Command("cmd", "/c", "start", u).Start()
	default:
		_ = exec.Command("xdg-open", u).Start()
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
		case snap.Kind == ghpr.KindJira:
			label = snap.IssueKey
			if label == "" {
				label = snap.Input
			}
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
		case snap.Kind == ghpr.KindJira:
			status := snap.State
			if status == "" {
				status = "unknown"
			}
			fmt.Fprintf(&b, "%s  %s  %s\n", label, status, snap.Title)
		default:
			sum := ghpr.Summarize(snap.Checks)
			fmt.Fprintf(&b, "%s  %s  %s\n", label, sum.Outcome(), counts(sum))
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func tick(d time.Duration) tea.Cmd {
	if d <= 0 {
		d = DefaultInterval
	}
	return tea.Tick(d, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m model) watchingTitle() string {
	pending := 0
	for _, row := range m.rows {
		if row.snap.Kind == ghpr.KindJira {
			continue
		}
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

	prs, tickets, runs := 0, 0, 0
	for _, row := range m.rows {
		switch row.snap.Kind {
		case ghpr.KindRun:
			runs++
		case ghpr.KindJira:
			tickets++
		default:
			prs++
		}
	}
	var bits []string
	switch {
	case prs > 0 && tickets > 0 && runs > 0:
		bits = append(bits, fmt.Sprintf("%d PR%s · %d jira · %d action%s", prs, plural(prs), tickets, runs, plural(runs)))
	case prs > 0 && tickets > 0:
		bits = append(bits, fmt.Sprintf("%d PR%s · %d jira", prs, plural(prs), tickets))
	case tickets > 0 && runs > 0:
		bits = append(bits, fmt.Sprintf("%d jira · %d action%s", tickets, runs, plural(runs)))
	case prs > 0 && runs > 0:
		bits = append(bits, fmt.Sprintf("%d PR%s · %d action%s", prs, plural(prs), runs, plural(runs)))
	case tickets > 0:
		bits = append(bits, fmt.Sprintf("%d jira", tickets))
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
	avail := m.width - lipgloss.Width(left) - 1
	if avail < 8 {
		return truncate(left, max(1, m.width))
	}
	if lipgloss.Width(right) > avail {
		plain := strings.Join(bits, " · ")
		right = muted.Render(truncate(plain, avail))
	}
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 0 {
		return truncate(left, max(1, m.width))
	}
	return left + strings.Repeat(" ", gap) + right
}

func (m model) watching() bool {
	return m.mode == modeWatch || m.mode == modeAdd
}

func (m *model) layout() {
	if m.width == 0 {
		return
	}
	m.input.SetWidth(max(8, m.width-8))
	vw := max(1, m.width)
	chrome := m.chromeHeight()
	vh := max(1, m.height-chrome)
	if !m.ready {
		m.viewport = viewport.New(vw, vh)
		m.ready = true
	} else {
		m.viewport.Width = vw
		m.viewport.Height = vh
	}
	m.showBody()
}

func (m model) chromeHeight() int {
	h := lipgloss.Height(m.headerLine()) + 1
	if m.status != "" && m.mode != modeAdd {
		h += 1 + lipgloss.Height(truncate(m.status, max(1, m.width)))
	}
	if m.mode == modeAdd {
		h += 1 + lipgloss.Height(m.viewAddPanel())
	} else {
		h += 1 + lipgloss.Height(m.helpLine())
	}
	return h
}

func (m model) cardWidth() int {
	// Lipgloss Width is content+padding; left/right borders add 2 columns.
	w := m.width
	if m.ready && m.viewport.Width > 0 {
		w = m.viewport.Width
	}
	return max(1, w-2)
}

func (m *model) showBody() {
	if !m.ready {
		return
	}
	m.viewport.SetContent(m.body())
	m.scrollToSelected()
}

func (m *model) scrollToSelected() {
	if !m.ready || len(m.rows) == 0 {
		return
	}
	top, height := m.selectedBounds()
	if height <= 0 {
		return
	}
	vis := m.viewport.Height
	if vis <= 0 {
		return
	}
	if height >= vis {
		m.viewport.SetYOffset(top)
		return
	}
	off := m.viewport.YOffset
	if top < off {
		m.viewport.SetYOffset(top)
		return
	}
	if top+height > off+vis {
		m.viewport.SetYOffset(max(0, top+height-vis))
	}
}

func (m model) selectedBounds() (top, height int) {
	if len(m.rows) == 0 || m.selected < 0 || m.selected >= len(m.rows) {
		return 0, 0
	}
	card := m.renderCard(m.selected, m.rows[m.selected], m.cardWidth())
	height = lipgloss.Height(card)
	for i, line := range strings.Split(m.body(), "\n") {
		if strings.Contains(line, "▶") {
			return i, height
		}
	}
	return 0, height
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
