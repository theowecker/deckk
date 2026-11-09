# deckk

All your Claude Code sessions in one terminal tab.

![deckk dashboard](docs/screenshot.png)

If you run a Claude session per feature, you end up with a row of tabs and no
idea which one is waiting on you. A restart loses all of them. deckk gives you
one tab instead: a dashboard of every session, a tab strip to switch between
the ones that are running, and a way to bring everything back after a reboot.

## Install

```sh
brew install --cask theowecker/tap/deckk
```

Then run `deckk`. It needs macOS and iTerm2, and uses tmux, which Homebrew
installs for you. deckk runs its own tmux server with its own config, so it
won't touch any tmux setup you already have.

## Use

Run `deckk` from any terminal. Running it again brings you back to the same
place.

| Key | Does |
| --- | --- |
| `↵` or double-click | open a session, or resume it if it isn't running |
| `⌃Space` | flip between the dashboard and your last session |
| `n` | start Claude in a folder |
| `x` | close a session (it stays resumable) |
| `o` / `O` | give one session, or all of them, its own iTerm2 tab |
| `m` / `M` | bring one session, or all of them, back into deckk |
| `R` | reopen everything that should be open |
| `p` / `r` | pin or rename a session |
| `v` | group by status or by project |
| `s` | settings |
| `?` | all keys |
| `q` | quit (sessions keep running) |

Click a tab in the strip to switch to it. On the dashboard, `1` to `9` jump to
a tab too.

Drag over text to copy it to the clipboard, or double-click a word. Hold
Option while dragging to get iTerm2's own selection instead.

## What it keeps track of

- **Status.** Each session shows whether Claude is working or waiting on you,
  and for how long. A session that finished while you were elsewhere is marked
  new until you open it.
- **Context.** The selected row shows your last prompt, Claude's latest reply
  and any pull request it opened.
- **Sessions in other tabs.** Claude sessions started outside deckk show up
  too, and `m` moves them in.
- **Restarts.** Sessions in deckk keep running if you quit iTerm2. After a
  reboot, the dashboard lists what was open, plus anything closed in the last
  day, and `R` reopens all of it.

## Settings

Press `s` to set the view, model, effort and permission mode for every Claude
that deckk starts. Settings live in `~/.config/deckk/config.toml`:

```toml
view = "focus"
model = "opus"
effort = ""
permission_mode = "acceptEdits"
extra_args = "--add-dir ~/shared"
days = 30
```

An empty value leaves it to your normal Claude settings. `extra_args` is added
to every `claude` command deckk runs.

## How it works

deckk reads what Claude Code already writes to disk:
`~/.claude/sessions/*.json` for running sessions and
`~/.claude/projects/*/*.jsonl` for transcripts. These formats aren't
documented, so a Claude Code update can break deckk. The parsing is all in
`internal/claude`.

The mouse pointer changes over clickable things on iTerm2 3.7 and later.

## Development

```sh
go test ./...
golangci-lint run
go build -o bin/deckk ./cmd/deckk && bin/deckk
script/screenshot   # rebuild docs/screenshot.png
```

Pushing a `v*` tag builds a release and updates the Homebrew tap.

## License

MIT
