# deckk

All your Claude Code sessions in one terminal tab.

![deckk dashboard](docs/screenshot.png)

If you run a Claude session per feature, you end up with a row of tabs and no
idea which one is waiting on you. A restart loses all of them. deckk gives you
one tab instead: a dashboard of every session, a tab strip to switch between
the ones that are running, and a way to bring everything back after a reboot.

## Install

```sh
brew tap theowecker/tap
brew trust --cask theowecker/tap/deckk
brew install --cask deckk
```

Homebrew asks you to trust casks from taps outside its main repo before it
will load them, which is what the middle line does.

Then run `deckk`. It needs macOS and uses tmux, which Homebrew installs for
you. deckk runs its own tmux server with its own config, so it won't touch any
tmux setup you already have.

## Use

Run `deckk` from any terminal. Running it again brings you back to the same
place.

| Key | Does |
| --- | --- |
| `↵` or double-click | open a session, or resume it if it isn't running |
| `⌃Space` | flip between the dashboard and your last session |
| `⌃⇧←` / `⌃⇧→` | previous or next tab (also `⌘⇧←` / `⌘⇧→` in iTerm2) |
| drag a tab | drop it on another tab to move it there |
| `n` | start Claude in a folder |
| `x` | close a session (it stays resumable) |
| `o` / `O` | give one session, or all of them, its own iTerm2 tab |
| `m` / `M` | bring one session, or all of them, back into deckk |
| `R` | reopen everything that should be open |
| `p` / `r` | pin or rename a session |
| `/` | find any session, ever |
| `tab` | switch between Sessions, Background and Find |
| `v` | group by status or by project |
| `s` | settings |
| `?` | all keys |
| `q` | quit (sessions keep running) |

Click a tab in the strip to switch to it. On the dashboard, `1` to `9` jump to
a tab too.

Drag over text to select it, or double-click a word, then press Ctrl+C to
copy. In iTerm2, Cmd+C works too. Click anywhere to clear the selection.

## Terminals

deckk runs in any terminal. In iTerm2, Ghostty 1.3+ and Terminal it can also
open, find and close tabs for you:

| When deckk runs in | iTerm2 | Ghostty | Terminal | Others |
| --- | --- | --- | --- | --- |
| Dashboard, tabs, resume, restore | yes | yes | yes | yes |
| `o` moves a session to its own tab | yes | yes | a window | no |
| Copy a selection | Cmd+C | Ctrl+C | Ctrl+C | Ctrl+C |
| Terminal's own selection | Option-drag | Shift-drag | Fn-drag | varies |

Claude sessions running in iTerm2, Ghostty or Terminal tabs show up on the
dashboard wherever deckk runs, and `↵` jumps to them.

The first time deckk controls a terminal, macOS asks whether to allow it.

## What it keeps track of

- **Status.** Each session shows whether Claude is working or waiting on you,
  and for how long. A session that finished while you were elsewhere is marked
  new until you open it.
- **Context.** The selected row shows your last prompt, Claude's latest reply
  and any pull request it opened. Under the tab strip, each tab shows how
  long it has been working or waiting, its folder, branch, uncommitted changes,
  commits ahead or behind, worktree and pull request, and on the right the
  model, effort, permission mode, context size and uptime.
- **Pins.** `p`, or the pin button under a tab, saves a session to the Pinned
  list, where it stays after you close it, however old it gets, until you
  unpin it. A pinned tab shrinks to its first word and moves up next to the
  deckk tab.
- **Sessions in other tabs.** Claude sessions started outside deckk show up
  too, and `m` moves them in.
- **Background runs.** Headless sessions from `claude -p`, plugins and
  scripts get their own Background view, so they don't crowd your own.
- **Restarts.** Sessions in deckk keep running if you quit your terminal. After a
  reboot, the dashboard lists what was open, plus anything closed in the last
  day, and `R` reopens all of it.

## Settings

Press `s` to set the view, model, effort and permission mode for every Claude
that deckk starts, and the keys that switch tabs. Settings live in
`~/.config/deckk/config.toml`:

```toml
view = "focus"
model = "opus"
effort = ""
permission_mode = "acceptEdits"
extra_args = "--add-dir ~/shared"
days = 30
tab_keys = ""
```

An empty value leaves it to your normal Claude settings. `extra_args` is added
to every `claude` command deckk runs. `tab_keys` is empty for ⌘⇧← ⌘⇧→,
`brackets` for ⌘⇧[ ⌘⇧], `pageup` for ⌃PgUp ⌃PgDn or `off`. The ⌘ keys take
over iTerm2's own use of them, but only in the deckk tab.

## How it works

deckk reads what Claude Code already writes to disk:
`~/.claude/sessions/*.json` for running sessions and
`~/.claude/projects/*/*.jsonl` for transcripts. These formats aren't
documented, so a Claude Code update can break deckk. The parsing is all in
`internal/claude`.

Each terminal deckk can drive is a few lines of AppleScript in
`internal/terminal/apps.go`, so adding one is a small change.

In iTerm2, the deckk tab uses a "deckk" profile so that Cmd+C goes to deckk.
The profile copies everything else from the profile you started in, except
iTerm2's own scrollback, which it turns off because tmux keeps each session's
history. deckk switches back when you quit. It lives in
`~/Library/Application Support/iTerm2/DynamicProfiles/deckk.json`.

The mouse pointer changes over clickable things in Ghostty and in iTerm2 3.7
and later.

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
