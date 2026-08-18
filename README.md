# chores

Terminal UI that watches GitHub PR checks, Actions runs, and Jira issues, then pings you when they change.

## Prerequisites

- **GitHub:** [`gh`](https://cli.github.com/) in `PATH`, and you are logged in (`gh auth login`).
- **Jira:** Slack desktop app with the Jira app installed, and Slack notifications enabled for that Jira app. chores reads those desktop notifications (no Atlassian API token). On macOS, grant Full Disk Access to your terminal (or `chores`) so it can read Notification Center.

## Install

```bash
go install github.com/abit2/chores@latest
```

Or from this repo:

```bash
make install
```

## Refresh interval

Polling defaults to **10 minutes**. Set it with `-i` / `--interval` (Go duration: `s`, `m`, `h`).

```bash
chores -i 30s
chores --interval 1m
chores --interval 10m https://github.com/org/repo/pull/123
```

`gh pr checks` and `gh pr view` count against GitHub's [GraphQL rate limits](https://docs.github.com/en/graphql/overview/rate-limits-and-query-limits-for-the-graphql-api) (5,000 points/hour, and a secondary cap on concurrent calls). Interval polls skip PRs/runs whose CI already finished; press `r` to refresh the selected card, or `R` to refresh all. If you hit a rate limit, wait for reset and use a slower `-i`.

## Watch list directory

Saved URLs live in a JSON file:

| OS | Default path |
| --- | --- |
| macOS | `~/Library/Application Support/chores/watch.json` |
| Linux | `~/.config/chores/watch.json` |

Override the file (and thus the directory) with `CHORES_WATCH_FILE`:

```bash
export CHORES_WATCH_FILE="$HOME/.chores/watch.json"
mkdir -p "$(dirname "$CHORES_WATCH_FILE")"
chores
```

Or for a single run:

```bash
CHORES_WATCH_FILE=./watch.json chores --repo owner/repo
```

`chores -h` prints the path currently in use.

Slack/Jira desktop notifications are appended to `notifications.json` in the same directory, grouped by issue key (`UI-1234`) in the order they were received. Override with `CHORES_NOTES_FILE`.

GitHub PR and Actions snapshots (title, checks, last refresh time) are saved to `prs.json` in the same directory. Override with `CHORES_PR_FILE`. Press `r` to refresh the selected card, `R` to refresh everything.

## Jira

Jira cards come from **Slack desktop notifications** posted by the Jira Slack app. chores does not call the Jira API.

On macOS, grant **Full Disk Access** to the terminal (or `chores`) in System Settings → Privacy & Security so it can read Notification Center. If that is blocked, it will try to read visible notification banners (Accessibility permission).

Pin a ticket if you want, or let chores pick up issue keys from incoming Slack/Jira notifications:

```bash
chores https://xyz-company.atlassian.net/browse/UI-5947
chores UI-5947
```

Bare keys use `JIRA_SITE` or `--jira-site` only to open the browse URL with `o`:

```bash
export JIRA_SITE="xyz-company.atlassian.net"
chores UI-5947
```

You'll get a ping when a new Slack notification arrives for that issue.

Press `c` to clear saved notifications for the selected Jira key, or `N` to clear all of them. Older Slack banners for those keys are ignored until a new notification arrives.

## Usage

```bash
chores
chores https://github.com/org/repo/pull/123
chores --repo owner/repo
chores https://xyz-company.atlassian.net/browse/UI-5947
```

Keys: `a` add · `d` remove · `C` clear list · `c` clear notes · `N` clear all notes · `j`/`k` select · `enter` expand · `o` open · `r` refresh · `R` refresh all · `q` quit
