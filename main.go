package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"

	"github.com/abit2/chores/internal/ghpr"
	"github.com/abit2/chores/internal/ui"
	tea "github.com/charmbracelet/bubbletea"
)

var version = "dev"

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
		fmt.Fprintf(os.Stderr, `Watch GitHub PR CI and Actions runs with gh, and ping you when they finish.

Usage:
  chores [flags] <pr-or-run-url> [<url> ...]

URLs can be pull requests or Actions run links, space- or comma-separated.
If none are given, the TUI lets you paste them. You can also pipe URLs on stdin.

  chores --repo owner/repo     watch recent Actions runs for a repository

Flags:
`)
		flag.PrintDefaults()
	}
	flag.Parse()

	if *printVersion {
		fmt.Println(version)
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
