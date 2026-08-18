package macnotify

import "testing"

func TestDecodeRequest(t *testing.T) {
	xml := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>app</key>
	<string>com.tinyspeck.slackmacgap</string>
	<key>req</key>
	<dict>
		<key>titl</key>
		<string>Jira</string>
		<key>subt</key>
		<string>xyz-company</string>
		<key>body</key>
		<string>Ada commented on UI-5947</string>
	</dict>
</dict>
</plist>`
	title, subtitle, body, app := decodeRequest([]byte(xml))
	if app != "com.tinyspeck.slackmacgap" {
		t.Fatalf("app=%q", app)
	}
	if title != "Jira" || subtitle != "xyz-company" || body != "Ada commented on UI-5947" {
		t.Fatalf("%q %q %q", title, subtitle, body)
	}
}

func TestIssueKeys(t *testing.T) {
	got := IssueKeys("Ada commented on UI-5947 and also UI-1")
	if len(got) != 2 || got[0] != "UI-5947" || got[1] != "UI-1" {
		t.Fatalf("%v", got)
	}
}

func TestLooksLikeJira(t *testing.T) {
	if !looksLikeJira(Note{Title: "Jira", Body: "hello"}) {
		t.Fatal("title jira")
	}
	if !looksLikeJira(Note{Body: "please see UI-9"}) {
		t.Fatal("issue key")
	}
	if looksLikeJira(Note{Title: "Slack", Body: "lunch?"}) {
		t.Fatal("plain slack should not match")
	}
}
