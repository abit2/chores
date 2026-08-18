package ghpr

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/abit2/chores/internal/jira"
)

// Check is one GitHub status check or Actions job from `gh pr checks --json`.
type Check struct {
	Name        string `json:"name"`
	State       string `json:"state"`
	Bucket      string `json:"bucket"`
	Workflow    string `json:"workflow"`
	Link        string `json:"link"`
	StartedAt   string `json:"startedAt"`
	CompletedAt string `json:"completedAt"`
	Description string `json:"description"`
}

// Kind identifies whether a snapshot is a pull request or a workflow run.
type Kind string

const (
	KindPR   Kind = "pr"
	KindRun  Kind = "run"
	KindJira Kind = "jira"
)

// Snapshot is the latest CI picture for one pull request or Actions run.
type Snapshot struct {
	Kind          Kind
	Input         string
	Number        int
	RunID         int64
	Title         string
	URL           string
	State         string
	IsDraft       bool
	HeadRefName   string
	Author        string
	Repo          string
	WorkflowName  string
	Event         string
	IssueKey      string
	IssueType     string
	Priority      string
	Updated       string
	LastCommenter string
	Checks        []Check
	Err           error
	FetchedAt     time.Time
}

// Summary counts checks by gh bucket.
type Summary struct {
	Pass    int
	Fail    int
	Pending int
	Skip    int
	Cancel  int
}

type prView struct {
	Number      int    `json:"number"`
	Title       string `json:"title"`
	URL         string `json:"url"`
	State       string `json:"state"`
	IsDraft     bool   `json:"isDraft"`
	HeadRefName string `json:"headRefName"`
	Author      struct {
		Login string `json:"login"`
	} `json:"author"`
}

var (
	prURLRe  = regexp.MustCompile(`(?i)(?:github\.com[:/])([^/]+)/([^/]+)/pull/(\d+)`)
	runURLRe = regexp.MustCompile(`(?i)(?:github\.com[:/])([^/]+)/([^/]+)/actions/runs/(\d+)`)
	// GitHub allows 100 concurrent API requests and 2,000 GraphQL points/minute.
	// Keep gh calls serial-ish so interval polls do not trip secondary limits.
	ghGate = make(chan struct{}, 2)
)

// ParseRefs splits CLI args, commas, and whitespace into PR refs (URLs or numbers).
func ParseRefs(args []string) []string {
	var refs []string
	seen := make(map[string]struct{})
	for _, arg := range args {
		for _, part := range strings.FieldsFunc(arg, func(r rune) bool {
			return r == ',' || unicode.IsSpace(r)
		}) {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			if _, ok := seen[part]; ok {
				continue
			}
			seen[part] = struct{}{}
			refs = append(refs, part)
		}
	}
	return refs
}

// RepoFromRef extracts owner/repo from a GitHub pull request or Actions run URL.
func RepoFromRef(ref string) string {
	if m := prURLRe.FindStringSubmatch(ref); len(m) == 4 {
		return m[1] + "/" + m[2]
	}
	if m := runURLRe.FindStringSubmatch(ref); len(m) == 4 {
		return m[1] + "/" + m[2]
	}
	return ""
}

// NumberFromRef extracts the pull request number from a GitHub URL.
func NumberFromRef(ref string) int {
	m := prURLRe.FindStringSubmatch(ref)
	if len(m) != 4 {
		return 0
	}
	n, _ := strconv.Atoi(m[3])
	return n
}

// RunIDFromRef extracts the workflow run ID from an Actions URL.
func RunIDFromRef(ref string) int64 {
	m := runURLRe.FindStringSubmatch(ref)
	if len(m) != 4 {
		return 0
	}
	n, _ := strconv.ParseInt(m[3], 10, 64)
	return n
}

// IsRunRef reports whether ref points at a GitHub Actions workflow run.
func IsRunRef(ref string) bool {
	return RunIDFromRef(ref) > 0
}

// Identity is the canonical repo + number for a pull request snapshot, when known.
func Identity(s Snapshot) (repo string, number int) {
	number = s.Number
	if number == 0 {
		number = NumberFromRef(s.URL)
		if number == 0 {
			number = NumberFromRef(s.Input)
		}
	}
	repo = s.Repo
	if repo == "" {
		repo = RepoFromRef(s.URL)
		if repo == "" {
			repo = RepoFromRef(s.Input)
		}
	}
	return repo, number
}

// SamePR reports whether two snapshots refer to the same pull request.
func SamePR(a, b Snapshot) bool {
	ra, na := Identity(a)
	rb, nb := Identity(b)
	if na > 0 && na == nb && ra != "" && ra == rb {
		return true
	}
	if a.URL != "" && b.URL != "" && strings.TrimRight(a.URL, "/") == strings.TrimRight(b.URL, "/") {
		return true
	}
	return a.Input != "" && a.Input == b.Input
}

// Same reports whether two snapshots refer to the same PR, Actions run, or Jira issue.
func Same(a, b Snapshot) bool {
	if isJiraSnap(a) || isJiraSnap(b) {
		return sameJira(a, b)
	}
	if a.RunID > 0 && a.RunID == b.RunID {
		return true
	}
	if IsRunRef(a.Input) && a.Input == b.Input {
		return true
	}
	if a.Kind == KindRun || b.Kind == KindRun {
		if a.URL != "" && b.URL != "" && strings.TrimRight(a.URL, "/") == strings.TrimRight(b.URL, "/") {
			return true
		}
		return false
	}
	return SamePR(a, b)
}

// Fetch loads PR or Actions run status via gh.
func Fetch(ctx context.Context, ref string, required bool) Snapshot {
	if jira.IsRef(ref) || strings.HasPrefix(ref, "slack:") {
		return snapshotFromJira(jira.Fetch(ctx, ref))
	}
	if IsRunRef(ref) {
		return FetchRun(ctx, ref)
	}
	return fetchPR(ctx, ref, required)
}

func fetchPR(ctx context.Context, ref string, required bool) Snapshot {
	snap := Snapshot{Kind: KindPR, Input: ref, FetchedAt: time.Now()}
	if repo := RepoFromRef(ref); repo != "" {
		snap.Repo = repo
	}
	snap = loadPRView(ctx, snap)
	if snap.Err != nil {
		return snap
	}
	return loadChecks(ctx, snap, required)
}

// Refresh reloads status for an already-fetched PR or Actions run.
func Refresh(ctx context.Context, prev Snapshot, required bool) Snapshot {
	if prev.Kind == KindJira || jira.IsRef(prev.Input) || jira.IsRef(prev.URL) || strings.HasPrefix(prev.Input, "slack:") {
		ref := prev.URL
		if ref == "" {
			ref = prev.Input
		}
		return snapshotFromJira(jira.Fetch(ctx, ref))
	}
	if prev.Kind == KindRun || prev.RunID > 0 || IsRunRef(prev.Input) {
		if prev.RunID > 0 && prev.Repo != "" {
			return loadRun(ctx, prev)
		}
		return FetchRun(ctx, prev.Input)
	}
	if prev.URL == "" || prev.Number == 0 {
		return Fetch(ctx, prev.Input, required)
	}
	next := prev
	next.Kind = KindPR
	next.Err = nil
	next.FetchedAt = time.Now()
	return loadChecks(ctx, next, required)
}

func loadChecks(ctx context.Context, snap Snapshot, required bool) Snapshot {
	ref := snap.URL
	if ref == "" {
		ref = snap.Input
	}
	args := []string{
		"pr", "checks", ref, "--json",
		"name,state,bucket,workflow,link,completedAt,startedAt,description",
	}
	if required {
		args = append(args, "--required")
	}
	out, err := runGH(ctx, args...)
	if err != nil {
		snap.Err = err
		return snap
	}
	if len(bytes.TrimSpace(out)) == 0 {
		snap.Checks = nil
		return snap
	}
	var checks []Check
	if err := json.Unmarshal(out, &checks); err != nil {
		snap.Err = fmt.Errorf("decode pr checks: %w", err)
		return snap
	}
	snap.Checks = checks
	return snap
}

func runGH(ctx context.Context, args ...string) ([]byte, error) {
	select {
	case ghGate <- struct{}{}:
		defer func() { <-ghGate }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	cmd := exec.CommandContext(ctx, "gh", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	out := stdout.Bytes()
	if len(bytes.TrimSpace(out)) > 0 {
		// gh pr checks exits 8 while pending and 1 on failure; JSON is still valid.
		return out, nil
	}
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		if isRateLimit(msg) {
			return nil, fmt.Errorf("github rate limit: wait until reset, or poll less often (-i); see https://docs.github.com/en/graphql/overview/rate-limits-and-query-limits-for-the-graphql-api")
		}
		return nil, fmt.Errorf("gh %s: %s", strings.Join(args, " "), msg)
	}
	return out, nil
}

func isRateLimit(msg string) bool {
	low := strings.ToLower(msg)
	return strings.Contains(low, "rate limit") || strings.Contains(low, "secondary rate")
}

// Summarize counts checks by bucket.
func Summarize(checks []Check) Summary {
	var s Summary
	for _, c := range checks {
		switch strings.ToLower(c.Bucket) {
		case "pass":
			s.Pass++
		case "fail":
			s.Fail++
		case "pending":
			s.Pending++
		case "skipping":
			s.Skip++
		case "cancel":
			s.Cancel++
		default:
			switch strings.ToUpper(c.State) {
			case "SUCCESS":
				s.Pass++
			case "FAILURE", "ERROR", "TIMED_OUT", "ACTION_REQUIRED":
				s.Fail++
			case "SKIPPED", "NEUTRAL":
				s.Skip++
			case "CANCELLED":
				s.Cancel++
			default:
				s.Pending++
			}
		}
	}
	return s
}

func (s Summary) Total() int {
	return s.Pass + s.Fail + s.Pending + s.Skip + s.Cancel
}

// Finished reports whether CI has a conclusive result (no pending jobs).
// An empty check list is treated as "not started yet", not finished.
func (s Summary) Finished() bool {
	return s.Total() > 0 && s.Pending == 0
}

func (s Summary) Passed() bool {
	return s.Finished() && s.Fail == 0 && s.Cancel == 0
}

func (s Summary) Outcome() string {
	if s.Total() == 0 {
		return "waiting for checks"
	}
	if s.Pending > 0 {
		return "running"
	}
	if s.Fail > 0 {
		return "failed"
	}
	if s.Cancel > 0 && s.Pass == 0 {
		return "cancelled"
	}
	if s.Pass == 0 && s.Skip > 0 {
		return "skipped"
	}
	return "passed"
}

// Duration formats how long a check has been running or took to complete.
func (c Check) Duration(now time.Time) string {
	start, err := time.Parse(time.RFC3339, c.StartedAt)
	if err != nil {
		return ""
	}
	end := now
	if t, err := time.Parse(time.RFC3339, c.CompletedAt); err == nil && !t.IsZero() {
		end = t
	}
	d := end.Sub(start)
	if d < 0 {
		return ""
	}
	return FormatDuration(d)
}

// FormatDuration renders a short human duration.
func FormatDuration(d time.Duration) string {
	if d < time.Second {
		return "0s"
	}
	d = d.Round(time.Second)
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	switch {
	case h > 0:
		return fmt.Sprintf("%dh %dm", h, m)
	case m > 0:
		return fmt.Sprintf("%dm %ds", m, s)
	default:
		return fmt.Sprintf("%ds", s)
	}
}
