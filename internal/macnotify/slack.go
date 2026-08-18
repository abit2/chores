package macnotify

import (
	"context"
	"regexp"
	"runtime"
	"strings"
)

var issueKeyPattern = regexp.MustCompile(`\b[A-Z][A-Z0-9]+-\d+\b`)

// Slack returns recent Slack desktop notifications.
func Slack(ctx context.Context) ([]Note, error) {
	if runtime.GOOS != "darwin" {
		return nil, nil
	}
	return slackDarwin(ctx)
}

func looksLikeJira(n Note) bool {
	blob := n.Text()
	if issueKeyPattern.MatchString(blob) {
		return true
	}
	low := strings.ToLower(blob)
	if strings.Contains(low, "jira") || strings.Contains(low, "atlassian.net") {
		return true
	}
	return false
}

// JiraNotes returns Slack notifications that look like Jira activity.
func JiraNotes(ctx context.Context) ([]Note, error) {
	notes, err := Slack(ctx)
	if err != nil {
		return nil, err
	}
	var out []Note
	for _, n := range notes {
		if looksLikeJira(n) {
			out = append(out, n)
		}
	}
	return out, nil
}

func IssueKeys(text string) []string {
	found := issueKeyPattern.FindAllString(text, -1)
	if len(found) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(found))
	var keys []string
	for _, k := range found {
		k = strings.ToUpper(k)
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		keys = append(keys, k)
	}
	return keys
}
