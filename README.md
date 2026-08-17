# chores

Terminal UI that watches GitHub PR checks and Actions runs with `gh`, then pings you when CI finishes.

Requires [`gh`](https://cli.github.com/) in `PATH` (`gh auth login`).

## Install

```bash
go install github.com/abit2/chores@latest
```

Or from this repo:

```bash
make install
```

## Refresh interval

Polling defaults to **10 seconds**. Set it with `-i` / `--interval` (Go duration: `s`, `m`, `h`).

```bash
chores -i 5s
chores --interval 30s
chores --interval 1m https://github.com/org/repo/pull/123
```

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

## Usage

```bash
chores
chores https://github.com/org/repo/pull/123
chores --repo owner/repo
```

Keys: `a` add · `d` remove · `C` clear saved URLs · `j`/`k` select · `enter` expand · `o` browser · `r` refresh · `q` quit
