package ghpr

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"
	"unicode"
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

// Snapshot is the latest CI picture for one pull request.
type Snapshot struct {
	Input       string
	Number      int
	Title       string
	URL         string
	State       string
	IsDraft     bool
	HeadRefName string
	Author      string
	Repo        string
	Checks      []Check
	Err         error
	FetchedAt   time.Time
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

var prURLRe = regexp.MustCompile(`(?i)(?:github\.com[:/])([^/]+)/([^/]+)/pull/(\d+)`)

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

// RepoFromRef extracts owner/repo from a GitHub pull request URL.
func RepoFromRef(ref string) string {
	m := prURLRe.FindStringSubmatch(ref)
	if len(m) != 4 {
		return ""
	}
	return m[1] + "/" + m[2]
}

// Fetch loads PR metadata and checks via gh.
func Fetch(ctx context.Context, ref string, required bool) Snapshot {
	snap := Snapshot{Input: ref, FetchedAt: time.Now()}
	if repo := RepoFromRef(ref); repo != "" {
		snap.Repo = repo
	}

	viewOut, err := runGH(ctx, "pr", "view", ref, "--json",
		"number,title,url,state,isDraft,headRefName,author")
	if err != nil {
		snap.Err = err
		return snap
	}

	var view prView
	if err := json.Unmarshal(viewOut, &view); err != nil {
		snap.Err = fmt.Errorf("decode pr view: %w", err)
		return snap
	}

	snap.Number = view.Number
	snap.Title = view.Title
	snap.URL = view.URL
	snap.State = view.State
	snap.IsDraft = view.IsDraft
	snap.HeadRefName = view.HeadRefName
	snap.Author = view.Author.Login
	if snap.Repo == "" {
		snap.Repo = RepoFromRef(view.URL)
	}

	return loadChecks(ctx, snap, required)
}

// Refresh reloads checks for an already-fetched PR. Metadata is refetched if
// the previous call failed.
func Refresh(ctx context.Context, prev Snapshot, required bool) Snapshot {
	if prev.URL == "" || prev.Number == 0 {
		return Fetch(ctx, prev.Input, required)
	}
	next := prev
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
		return nil, fmt.Errorf("gh %s: %s", strings.Join(args, " "), msg)
	}
	return out, nil
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
