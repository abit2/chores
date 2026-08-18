package jira

import (
	"context"
	"crypto/sha1"
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/abit2/chores/internal/macnotify"
)

var (
	browseRe = regexp.MustCompile(`(?i)^https?://([^/]+)/browse/([A-Z][A-Z0-9]+-\d+)`)
	selectRe = regexp.MustCompile(`(?i)[?&]selectedIssue=([A-Z][A-Z0-9]+-\d+)`)
	keyRe    = regexp.MustCompile(`(?i)^[A-Z][A-Z0-9]+-\d+$`)
	hostRe   = regexp.MustCompile(`(?i)^https?://([^/]+)`)
)

// Ref is a parsed Jira browse URL or issue key.
type Ref struct {
	Host string
	Key  string
}

// Issue is the subset of a Jira ticket shown in the watcher.
type Issue struct {
	Input         string
	Host          string
	Key           string
	Title         string
	URL           string
	Status        string
	Type          string
	Priority      string
	Assignee      string
	Updated       string
	LastCommenter string
	Err           error
	FetchedAt     time.Time
}

// Parse extracts host and issue key from a browse URL, selectedIssue URL, or KEY-123.
func Parse(raw string) (Ref, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Ref{}, false
	}
	if m := browseRe.FindStringSubmatch(raw); len(m) == 3 {
		return Ref{Host: strings.ToLower(m[1]), Key: strings.ToUpper(m[2])}, true
	}
	if m := selectRe.FindStringSubmatch(raw); len(m) == 2 {
		ref := Ref{Key: strings.ToUpper(m[1])}
		if h := hostRe.FindStringSubmatch(raw); len(h) == 2 {
			ref.Host = strings.ToLower(h[1])
		}
		if ref.Host == "" {
			ref.Host = defaultHost()
		}
		return ref, true
	}
	if keyRe.MatchString(raw) {
		return Ref{Key: strings.ToUpper(raw), Host: defaultHost()}, true
	}
	return Ref{}, false
}

// IsRef reports whether raw looks like a Jira issue URL or key.
func IsRef(raw string) bool {
	_, ok := Parse(raw)
	return ok
}

func defaultHost() string {
	h := firstEnv("JIRA_SITE", "JIRA_HOST", "ATLASSIAN_SITE")
	h = strings.TrimSpace(h)
	h = strings.TrimPrefix(h, "https://")
	h = strings.TrimPrefix(h, "http://")
	return strings.TrimRight(strings.ToLower(h), "/")
}

func (r Ref) BrowseURL() string {
	if r.Host == "" || r.Key == "" {
		return ""
	}
	return "https://" + r.Host + "/browse/" + r.Key
}

var (
	noteMu    sync.Mutex
	noteCache []macnotify.Note
	noteErr   error
	noteAt    time.Time
	noteLog   noteFile
)

func slackNotes(ctx context.Context) ([]macnotify.Note, error) {
	noteMu.Lock()
	defer noteMu.Unlock()
	if !noteAt.IsZero() && time.Since(noteAt) < 2*time.Second {
		return noteCache, noteErr
	}
	notes, err := macnotify.JiraNotes(ctx)
	if path, pathErr := NotesPath(); pathErr == nil {
		if log, loadErr := loadNoteFile(path); loadErr == nil {
			noteLog = log
		}
	}
	noteCache, noteErr, noteAt = notes, err, time.Now()
	if err == nil {
		recordNotes(notes)
	}
	return notes, err
}

// Fetch fills an issue from the latest matching Slack desktop notification.
func Fetch(ctx context.Context, raw string) Issue {
	ref, ok := Parse(raw)
	issue := Issue{
		Input:     raw,
		Host:      ref.Host,
		Key:       ref.Key,
		URL:       ref.BrowseURL(),
		FetchedAt: time.Now(),
	}
	if !ok && !strings.HasPrefix(raw, "slack:") {
		issue.Err = fmt.Errorf("not a Jira issue: %s", raw)
		return issue
	}
	notes, err := slackNotes(ctx)
	if err != nil {
		issue.Err = err
		return issue
	}
	if ok {
		applyNotes(&issue, notes)
		return issue
	}
	for _, n := range notes {
		if slackID(n) == raw {
			fillFromNote(&issue, n)
			return issue
		}
	}
	issue.Status = "waiting for Slack"
	return issue
}

// Discover returns Jira issues seen in recent Slack notifications that are not in knownKeys.
func Discover(ctx context.Context, knownKeys map[string]bool) ([]Issue, error) {
	notes, err := slackNotes(ctx)
	if err != nil {
		return nil, err
	}
	grouped := groupNotes(notes)
	var out []Issue
	for _, g := range grouped {
		if g.Key != "" && knownKeys[g.Key] {
			continue
		}
		if g.Key == "" && knownKeys[g.Input] {
			continue
		}
		out = append(out, g)
	}
	return out, nil
}

func applyNotes(issue *Issue, notes []macnotify.Note) {
	var latest macnotify.Note
	found := false
	for _, n := range notes {
		if issue.Key != "" && !noteHasKey(n, issue.Key) {
			continue
		}
		if issue.Key != "" && isDropped(issue.Key, n.Delivered) {
			continue
		}
		if !found || n.Delivered.After(latest.Delivered) {
			latest = n
			found = true
		}
	}
	if !found {
		if issue.Title == "" {
			issue.Status = "waiting for Slack"
		}
		return
	}
	fillFromNote(issue, latest)
}

func fillFromNote(issue *Issue, n macnotify.Note) {
	if issue.Key == "" {
		if keys := macnotify.IssueKeys(n.Text()); len(keys) > 0 {
			issue.Key = keys[0]
		}
	}
	if issue.Host == "" {
		issue.Host = defaultHost()
	}
	if issue.URL == "" {
		issue.URL = Ref{Host: issue.Host, Key: issue.Key}.BrowseURL()
	}
	issue.Title = noteTitle(n)
	issue.Status = "via Slack"
	issue.Type = "Slack"
	if !n.Delivered.IsZero() {
		issue.Updated = n.Delivered.UTC().Format(time.RFC3339)
	}
	issue.LastCommenter = strings.TrimSpace(n.Body)
	if issue.Input == "" {
		if issue.URL != "" {
			issue.Input = issue.URL
		} else if issue.Key != "" {
			issue.Input = issue.Key
		} else {
			issue.Input = slackID(n)
		}
	}
}

func groupNotes(notes []macnotify.Note) []Issue {
	latest := map[string]macnotify.Note{}
	order := make([]string, 0)
	for _, n := range notes {
		keys := macnotify.IssueKeys(n.Text())
		if len(keys) == 0 {
			id := slackID(n)
			if prev, ok := latest[id]; !ok || n.Delivered.After(prev.Delivered) {
				if !ok {
					order = append(order, id)
				}
				latest[id] = n
			}
			continue
		}
		for _, key := range keys {
			if isDropped(key, n.Delivered) {
				continue
			}
			if prev, ok := latest[key]; !ok || n.Delivered.After(prev.Delivered) {
				if !ok {
					order = append(order, key)
				}
				latest[key] = n
			}
		}
	}
	out := make([]Issue, 0, len(order))
	for _, id := range order {
		n := latest[id]
		issue := Issue{FetchedAt: time.Now()}
		if keyRe.MatchString(id) {
			issue.Key = id
			issue.Host = defaultHost()
			issue.URL = Ref{Host: issue.Host, Key: issue.Key}.BrowseURL()
			if issue.URL != "" {
				issue.Input = issue.URL
			} else {
				issue.Input = issue.Key
			}
		} else {
			issue.Input = id
		}
		fillFromNote(&issue, n)
		out = append(out, issue)
	}
	return out
}

func noteHasKey(n macnotify.Note, key string) bool {
	key = strings.ToUpper(key)
	for _, k := range macnotify.IssueKeys(n.Text()) {
		if k == key {
			return true
		}
	}
	return false
}

func noteTitle(n macnotify.Note) string {
	body := strings.TrimSpace(n.Body)
	if body != "" {
		return body
	}
	if s := strings.TrimSpace(n.Subtitle); s != "" {
		return s
	}
	return strings.TrimSpace(n.Title)
}

func slackID(n macnotify.Note) string {
	sum := sha1.Sum([]byte(n.Title + "\n" + n.Subtitle + "\n" + n.Body))
	return fmt.Sprintf("slack:%x", sum[:8])
}

func firstEnv(keys ...string) string {
	for _, k := range keys {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	return ""
}
