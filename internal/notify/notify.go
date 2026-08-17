package notify

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// Alert pings the user that CI finished: terminal bell, desktop notification, optional sound.
func Alert(title, body string, sound bool) {
	fmt.Fprint(os.Stderr, "\a")

	switch runtime.GOOS {
	case "darwin":
		notifyDarwin(title, body, sound)
	case "linux":
		notifyLinux(title, body)
	}
}

func notifyDarwin(title, body string, sound bool) {
	script := fmt.Sprintf("display notification %s with title %s", quoteAS(body), quoteAS(title))
	if sound {
		script += ` sound name "Glass"`
	}
	_ = exec.Command("osascript", "-e", script).Run()
}

func notifyLinux(title, body string) {
	if _, err := exec.LookPath("notify-send"); err != nil {
		return
	}
	_ = exec.Command("notify-send", title, body).Run()
}

func quoteAS(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}
