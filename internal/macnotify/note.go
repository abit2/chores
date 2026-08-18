package macnotify

import (
	"strings"
	"time"
)

// Note is one desktop notification we were able to read.
type Note struct {
	Bundle    string
	Title     string
	Subtitle  string
	Body      string
	Delivered time.Time
}

// Text joins the visible notification fields.
func (n Note) Text() string {
	var parts []string
	for _, s := range []string{n.Title, n.Subtitle, n.Body} {
		s = strings.TrimSpace(s)
		if s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, "\n")
}

func isSlackBundle(id string) bool {
	id = strings.ToLower(id)
	return strings.Contains(id, "slack") || strings.Contains(id, "tinyspeck")
}
