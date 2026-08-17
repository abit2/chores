package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"

	"github.com/abit2/chores/internal/ghpr"
	"github.com/abit2/chores/internal/store"
	"github.com/abit2/chores/internal/ui"
	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	var interval time.Duration
	flag.DurationVar(&interval, "interval", 10*time.Second, "how often to poll CI")
	flag.DurationVar(&interval, "i", 10*time.Second, "how often to poll CI")
	repo := flag.String("repo", "", "watch GitHub Actions runs for OWNER/REPO")
	flag.StringVar(repo, "R", "", "watch GitHub Actions runs for OWNER/REPO")
	required := flag.Bool("required", false, "only watch required checks")
	noSound := flag.Bool("no-sound", false, "notify without playing a sound")
	exitDone := flag.Bool("exit-when-done", false, "quit once every PR's CI has finished")
	printVersion := flag.Bool("version", false, "print version and exit")
	flag.Usage = func() {
		path, _ := store.Path()
		if path == "" {
			path = "$CHORES_WATCH_FILE or user config dir/chores/watch.json"
		}
		fmt.Fprintf(os.Stderr, `Watch GitHub PR CI and Actions runs with gh, and ping you when they finish.

Usage:
  chores [flags] <pr-or-run-url> [<url> ...]

URLs can be pull requests or Actions run links, space- or comma-separated.
If none are given, saved URLs are loaded. You can also paste them in the TUI
or pipe URLs on stdin.

  chores --repo owner/repo     watch recent Actions runs for a repository

Watch list: %s

Flags:
`, path)
		flag.PrintDefaults()
	}
	flag.Parse()

	if *printVersion {
		fmt.Println(resolveVersion())
		os.Exit(0)
	}

	if _, err := exec.LookPath("gh"); err != nil {
		fmt.Fprintln(os.Stderr, "gh is required in PATH (https://cli.github.com)")
		os.Exit(1)
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

	if interval <= 0 {
		interval = 10 * time.Second
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
