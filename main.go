package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"

	"github.com/abit2/chores/internal/ghpr"
	"github.com/abit2/chores/internal/jira"
	"github.com/abit2/chores/internal/store"
	"github.com/abit2/chores/internal/ui"
	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	var interval time.Duration
	flag.DurationVar(&interval, "interval", ui.DefaultInterval, "how often to poll")
	flag.DurationVar(&interval, "i", ui.DefaultInterval, "how often to poll")
	repo := flag.String("repo", "", "watch GitHub Actions runs for OWNER/REPO")
	flag.StringVar(repo, "R", "", "watch GitHub Actions runs for OWNER/REPO")
	jiraSite := flag.String("jira-site", "", "Jira host for bare issue keys (or set JIRA_SITE)")
	required := flag.Bool("required", false, "only watch required checks")
	noSound := flag.Bool("no-sound", false, "notify without playing a sound")
	exitDone := flag.Bool("exit-when-done", false, "quit once every PR's CI has finished")
	printVersion := flag.Bool("version", false, "print version and exit")
	flag.Usage = func() {
		path, _ := store.Path()
		if path == "" {
			path = "$CHORES_WATCH_FILE or user config dir/chores/watch.json"
		}
		notes, _ := jira.NotesPath()
		if notes == "" {
			notes = "$CHORES_NOTES_FILE or user config dir/chores/notifications.json"
		}
		prs, _ := store.PRsPath()
		if prs == "" {
			prs = "$CHORES_PR_FILE or user config dir/chores/prs.json"
		}
		fmt.Fprintf(os.Stderr, `Watch GitHub PR CI, Actions runs, and Jira issues from Slack notifications.

Usage:
  chores [flags] <pr-run-or-jira-url> [<url> ...]

URLs can be pull requests, Actions run links, or Jira browse links, space- or
comma-separated. If none are given, saved URLs are loaded. You can also paste
them in the TUI or pipe URLs on stdin.

  chores --repo owner/repo     watch recent Actions runs for a repository
  chores https://xyz-company.atlassian.net/browse/UI-5947

Jira is filled from Slack desktop notifications (the Jira Slack app), not the
Jira API. On macOS, grant Full Disk Access to your terminal so chores can read
Notification Center. Bare keys like UI-5947 can use JIRA_SITE / --jira-site
to open the browse URL.

Watch list: %s
Jira notifications: %s
GitHub snapshots: %s

Flags:
`, path, notes, prs)
		flag.PrintDefaults()
	}
	flag.Parse()

	if *printVersion {
		fmt.Println(resolveVersion())
		os.Exit(0)
	}

	if *jiraSite != "" && os.Getenv("JIRA_SITE") == "" {
		_ = os.Setenv("JIRA_SITE", *jiraSite)
	}

	refs := ghpr.ParseRefs(flag.Args())
	if len(refs) == 0 {
		if fi, err := os.Stdin.Stat(); err == nil && fi.Mode()&os.ModeCharDevice == 0 {
			b, err := io.ReadAll(os.Stdin)
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			refs = ghpr.ParseRefs([]string{string(b)})
		}
	}

	saved, err := store.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: watch list: %v\n", err)
	}
	refs = store.MergeURLs(saved.URLs, refs)
	if *repo == "" {
		*repo = saved.Repo
	}

	if needsGitHub(refs, *repo) {
		if _, err := exec.LookPath("gh"); err != nil {
			fmt.Fprintln(os.Stderr, "gh is required in PATH (https://cli.github.com)")
			os.Exit(1)
		}
	}

	if interval <= 0 {
		interval = ui.DefaultInterval
	}

	opts := []tea.ProgramOption{tea.WithAltScreen()}
	if fi, err := os.Stdin.Stat(); err == nil && fi.Mode()&os.ModeCharDevice == 0 {
		if tty, err := os.Open("/dev/tty"); err == nil {
			defer tty.Close()
			opts = append(opts, tea.WithInput(tty))
		}
	}

	p := tea.NewProgram(
		ui.New(ui.Config{
			Refs:     refs,
			Repo:     *repo,
			Hidden:   saved.Hidden,
			Interval: interval,
			Required: *required,
			NoSound:  *noSound,
			ExitDone: *exitDone,
		}),
		opts...,
	)
	final, err := p.Run()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if s := ui.ExitSummary(final); s != "" {
		fmt.Println(s)
	}
}

func needsGitHub(refs []string, repo string) bool {
	if repo != "" {
		return true
	}
	for _, ref := range refs {
		if !jira.IsRef(ref) {
			return true
		}
	}
	return false
}
