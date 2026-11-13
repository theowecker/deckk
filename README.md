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
| `c` | talk to the deckk session |
| `⌃⇧↑` / `⌃⇧↓` | move between the list and the deckk session |
| `d` | dismiss deckk's suggestion for a session |
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

## The deckk session

Next to the list runs one more Claude, the deckk session, whose job is the
sessions themselves. It sees every session, what each one just did and a goals
file you keep with it, so you can ask it what needs you, where you left off on
a project, or what to do next across all of them. Click it or press `c` to
talk to it.

It also works on its own:

- When a session finishes a turn, opens a PR or closes, deckk tells it.
- When the next step for a session is clear, it suggests a prompt. deckk types
  the prompt into that session while you're looking elsewhere, without sending
  it, so it's ready when you switch over. Clear it, edit it or press Enter.
  Sessions with a suggestion are listed under Next up and marked ✦.
- Sessions deckk starts can ask it what to do next with an `ask_deckk` tool,
  and wait for its answer, or leave it a note with `tell_deckk`.

It never types into a prompt you've started, into the tab you're on, or into
the deckk session while you're talking to it. Every update it gets is a turn
it spends, so it hears about them at most every 90 seconds, batched. Its
files, including `goals.md` and a `CLAUDE.md` you can add to, are in
`~/.config/deckk/hq`. The first time it starts, Claude asks you to trust that
folder.

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
deckk_session = ""
suggestions = ""
updates = ""
sessions_ask = ""
```

An empty value leaves it to your normal Claude settings. `extra_args` is added
to every `claude` command deckk runs. `tab_keys` is empty for ⌘⇧← ⌘⇧→,
`brackets` for ⌘⇧[ ⌘⇧], `pageup` for ⌃PgUp ⌃PgDn or `off`. The ⌘ keys take
over iTerm2's own use of them, but only in the deckk tab.

`deckk_session = "off"` turns the deckk session off. `suggestions` is empty
to type suggestions in for you to send, `send` to also send the ones deckk
marks as safe, or `list` to only list them under Next up. `updates = "off"`
stops telling the deckk session about changes, so it only acts when you ask.
`sessions_ask = "off"` leaves the `ask_deckk` tools out of sessions deckk
starts.

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
history, and triggers inside full-screen apps, which slow iTerm2 down as tmux
redraws. deckk switches back when you quit. It lives in
`~/Library/Application Support/iTerm2/DynamicProfiles/deckk.json`.

The deckk session gets its tools from `deckk mcp`, a small MCP server in
`internal/hq`. The same server gives other sessions `ask_deckk` and
`tell_deckk`, choosing by the folder it starts in. Suggestions, the inbox and
questions are plain files in `~/.config/deckk/hq`.

The mouse pointer changes over clickable things in Ghostty and in iTerm2 3.7
and later.

## Development

```sh
go test ./...
golangci-lint run
go build -o bin/deckk ./cmd/deckk && bin/deckk
script/screenshot   # rebuild docs/screenshot.png
```

`DECKK_SOCKET=deckk-dev` runs a second deckk on its own tmux server beside
your real one. Add `XDG_CONFIG_HOME` pointing at a scratch folder to keep its
settings and state apart too.

Pushing a `v*` tag builds a release and updates the Homebrew tap.

## License

MIT
