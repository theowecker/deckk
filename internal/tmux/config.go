package tmux

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/theowecker/deckk/internal/config"
	"github.com/theowecker/deckk/internal/terminal"
)

const glyph = "#{?#{==:#{@deckk_status},home},#[fg=#E8875B]◆," +
	"#{?#{==:#{@deckk_status},busy},#[fg=#7AA2F7]#{@deckk_reel}," +
	"#{?#{||:#{==:#{@deckk_status},idle},#{==:#{@deckk_status},fresh}},#[fg=#E8875B]●," +
	"#[fg=#6B6B6B]○}}}"

// The active tab is a solid chip, so its glyph drops its color to stay legible.
const plainGlyph = "#{?#{==:#{@deckk_status},home},◆," +
	"#{?#{==:#{@deckk_status},busy},#{@deckk_reel}," +
	"#{?#{||:#{==:#{@deckk_status},idle},#{==:#{@deckk_status},fresh}},●,○}}}"

const name = "#{?@deckk_name,#{@deckk_name},#W}"

const held = "#9A5A40"

// tmux owns the mouse, so the terminal never has a selection for Cmd+C. This
// copies like iTerm2 does, leaving the selection highlighted.
const copied = `{ send -X copy-pipe-no-clear ; display-message -d 1200 "Copied" }`

const copyIfSelected = `{ if -F "#{selection_present}" ` + copied + ` }`

// F12 rather than n, so the picker opens even while the filter has focus.
// F11 asks the dashboard to pin or unpin the tab you are on.
const clickStatus = `{ if -F "#{==:#{mouse_status_range},new}" { select-window -t :0 ; send-keys -t :0 F12 } { if -F "#{==:#{mouse_status_range},info}" ` + toggleInfo + ` { if -F "#{==:#{mouse_status_range},pin}" { send-keys -t :0 F11 } { select-window -t = } } } }`

// Dropping a tab on another moves it to that spot. Drag events only report
// the tab the drag began on, so this waits for the drag end, which reports
// the tab under the pointer. The dashboard stays first because deckk finds
// it at index 0, and run -C expands the formats move-window can't. The move
// makes every tab's place stale until deckk measures again.
const dropTab = `{ set -gu @deckk_drag ; if -F "#{&&:#{active_window_index},#{!=:#{window_index},#{active_window_index}}}" { run -C "move-window -#{?#{e|>:#{window_index},#{active_window_index}},a,b} -s :#{active_window_index} -t :#{?#{window_index},#{window_index},1} ; move-window -r ; #{W:set -wu -t #{window_id} @deckk_x ; }" } }`

const dragTab = `{ if -F "#{window_index}" { set -gF @deckk_drag "#{mouse_x}" } }`

const hovered = "#{&&:#{@deckk_x},#{&&:#{@deckk_drag},#{&&:#{e|>=:#{@deckk_drag},#{@deckk_x}},#{e|<:#{@deckk_drag},#{@deckk_x2}}}}}"

const dropTarget = "#{&&:#{!=:#{window_active},1}," + hovered + "}"

const toggleInfo = `{ if -F "#{==:#{status},4}" { set -g status 3 } { set -g status 4 } }`

const infoButton = `#[range=user|info]#[bg=#30304A,fg=#8A8A9A]  #{?#{==:#{status},4},▴,▾}  #[norange]#[default] `

// Claude never resets the pointer the dashboard sets. Switches the dashboard
// makes have no client tty, so it resets the pointer itself before those.
const resetPointer = `{ if -F "#{client_tty}" { run-shell -b "printf '\\033]22;\\007' > '#{client_tty}' 2>/dev/null; true" } }`

var tmuxConf = `
set -g default-terminal "tmux-256color"
set -s terminal-features[90] "xterm*:RGB:extkeys:clipboard:hyperlinks"
set -g extended-keys on
set -g escape-time 0
set -g focus-events on
set -g allow-passthrough on
set -g set-clipboard on
set -g history-limit 50000
set -g mouse on
# tmux can't hide its copy cursor, so it becomes a bar in the terminal's
# background colour.
setw -gq copy-mode-position-format ""
setw -gq copy-mode-mark-style default
set-hook -g pane-mode-changed { if -F "#{&&:#{pane_in_mode},#{@deckk_bg}}" { set -p cursor-style bar ; set -pF cursor-colour "#{@deckk_bg}" } { set -pu cursor-style ; set -pu cursor-colour } }
# deckk never needs the prefix, and Claude uses Ctrl+B itself.
set -g prefix None
set -g prefix2 None
unbind C-b
set -g copy-command "pbcopy"
set -s user-keys[0] "` + strings.ReplaceAll(terminal.CopySequence, "\x1b", `\e`) + `"
bind -n User0 { if -F "#{pane_in_mode}" ` + copyIfSelected + ` }
set -s user-keys[1] "` + strings.ReplaceAll(terminal.FindSequence, "\x1b", `\e`) + `"
set -s user-keys[2] "` + strings.ReplaceAll(terminal.FindNextSequence, "\x1b", `\e`) + `"
bind -n User1 { if -F "#{==:#{@deckk_pane},dashboard}" { send-keys / } { copy-mode ; ` + findPrompt + ` } }
bind -n User2 { if -F "#{pane_in_mode}" { send -X search-again } }
bind -n MouseDown1Pane { ` + noteClick + ` ; select-pane -t = ; send -M }
bind -n DoubleClick1Pane ` + sameClick(toApp(`select-pane -t = ; copy-mode -H ; send -X select-word`)) + `
bind -n TripleClick1Pane ` + sameClick(toApp(`select-pane -t = ; copy-mode -H ; send -X select-line`)) + `
bind -n MouseDrag1Pane ` + toApp(`copy-mode -M`) + `
# Only the dashboard window splits, for the deckk session, so the mouse can
# resize that split but never makes or moves panes.
unbind -n C-MouseDrag1Pane
unbind -n M-MouseDrag1Pane
bind -n MouseDrag1Border resize-pane -M
unbind -n M-MouseDown3Pane
unbind -n MouseDown3StatusLeft
unbind -n M-MouseDown3Status
unbind -n MouseDown3Status
bind -n MouseDown3Pane { select-pane -t = ; if -F "#{||:#{mouse_any_flag},#{==:#{@deckk_pane},dashboard}}" { send -M } { ` + rightMenu + ` } }
bind -n WheelUpPane ` + toApp(`if -F "#{alternate_on}" { send -M } { copy-mode -e }`) + `
` + copyModeBindings("copy-mode") + copyModeBindings("copy-mode-vi") + `set -g base-index 0
set -g renumber-windows on
set -g set-titles on
set -g set-titles-string "deckk"
setw -g allow-rename off
setw -g automatic-rename off
setw -g monitor-bell on

set -g status 4
set -g @deckk_cap_tab "#[fg=#{?` + dropTarget + `,#E8875B,#30304A}]#{@deckk_cap}"
set -g @deckk_cap_current "#[fg=#{?@deckk_drag,` + held + `,#E8875B}]#{@deckk_cap}"
set -g @deckk_base_tab "#[fg=#{?` + dropTarget + `,#E8875B,#30304A}]#{s/▄/▀/:@deckk_cap}"
set -g @deckk_base_current "#[fg=#{?@deckk_drag,` + held + `,#E8875B}]#{s/▄/▀/:@deckk_cap}"
set -g status-format[0] "` + strip("@deckk_cap_tab", "@deckk_cap_current", buttonCaps("▄")) + `"
set -g status-format[1] "` + strip("window-status-format", "window-status-current-format", "#{T:status-right}") + `"
set -g status-format[2] "` + strip("@deckk_base_tab", "@deckk_base_current", buttonCaps("▀")) + `"
set -g status-format[3] "#{?@deckk_drag,#[fill=#24243A bg=#24243A fg=#E8875B]  ⇄  #[fg=#B4B4C0]drop on a tab to move it there,#{?@deckk_info,#[fill=#24243A bg=#24243A]#{@deckk_info},}}"
set -g status-position top
set -g status-justify left
set -g status-style "bg=default,fg=default"
set -g status-left " "
set -g status-left-length 2
set -g status-right "` + infoButton + `#[range=user|new]#[bg=#30304A,fg=#E8875B,bold]  + new  #[norange]#[default] "
set -g status-right-length 60
set -g window-status-separator " "
set -g window-status-style "default"
set -g window-status-current-style "default"
set -g window-status-last-style "default"
set -g window-status-activity-style "default"
set -g window-status-bell-style "default"
set -g window-status-format "#[bg=#{?` + dropTarget + `,#4A4A6E,#30304A}]  ` + glyph + `#{?#{==:#{@deckk_status},fresh},#[fg=#E8875B#,bold],#[fg=#B4B4C0]} ` + name + `  #[default]"
set -g window-status-current-format "#[bg=#{?@deckk_drag,` + held + `,#E8875B},fg=#1E1E2E,bold]  ` + plainGlyph + ` ` + name + `  #[default]"
set -g message-style "bg=default,fg=#E8875B"
set -g pane-border-style "fg=#3A3A3A"
set -g pane-active-border-style "fg=#E8875B"

bind -n MouseDown1Status ` + clickStatus + `
bind -n MouseDown1StatusRight ` + clickStatus + `
bind -n MouseDrag1Status ` + dragTab + `
bind -n MouseDragEnd1Status ` + dropTab + `
bind -n MouseDragEnd1Pane { if -F "#{@deckk_drag}" { set -gu @deckk_drag } { send -M } }
bind -n C-Space if -F "#{!=:#{session_name},deckk}" "" { if -F "#{@deckk_dashboard}" "last-window" "select-window -t :0" }
# Outside the dashboard these reach the app.
bind -n C-S-Up ` + otherPane("C-S-Up") + `
bind -n C-S-Down ` + otherPane("C-S-Down") + `

set-hook -g session-window-changed ` + resetPointer + `
set-hook -g client-detached ` + resetPointer + `
# A tab never opens on a leftover selection.
set-hook -g client-attached { copy-mode -q }
`

// The label row sits between two rows of half blocks in each tab's colour,
// which make the tabs look twice as tall.
func strip(tab, current, right string) string {
	sep := "#{?loop_last_flag,,#{E:window-status-separator}}"
	return "#[align=left range=left]#{T:status-left}#[norange default]#[list=on align=left]" +
		"#{W:#[range=window|#{window_index}]#[push-default]#{T:" + tab + "}#[pop-default]#[norange default]" + sep +
		",#[range=window|#{window_index} list=focus]#[push-default]#{T:" + current + "}#[pop-default]#[norange list=on default]" + sep + "}" +
		"#[nolist align=right range=right]" + right + "#[norange default]"
}

func buttonCaps(half string) string {
	return "#[range=user|info]#[fg=#30304A]" + strings.Repeat(half, 5) + "#[norange default] " +
		"#[range=user|new]#[fg=#30304A]" + strings.Repeat(half, 9) + "#[norange default] "
}

func Cap(name string) string {
	return strings.Repeat("▄", ansi.StringWidth(strings.ReplaceAll(name, "##", "#"))+6)
}

// Ctrl+Shift+arrows reach tmux from any terminal and nothing else uses them.
func BindTabKeys(preset string) error {
	args := []string{}
	for _, key := range []string{"C-S-Left", "C-S-Right", "C-PPage", "C-NPage"} {
		args = append(args, "unbind", "-q", "-n", key, ";")
	}
	var prev, next string
	switch preset {
	case config.TabKeysArrows, config.TabKeysBrackets:
		prev, next = "C-S-Left", "C-S-Right"
	case config.TabKeysPage:
		prev, next = "C-PPage", "C-NPage"
	}
	if prev != "" {
		args = append(args, "bind", "-n", prev, "previous-window", ";", "bind", "-n", next, "next-window")
	}
	_, err := run(args...)
	return err
}

func WriteConfig() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, "deckk", Socket+".conf")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	return path, os.WriteFile(path, []byte(tmuxConf), 0o644)
}

// The dashboard never enters copy mode: it has no history, and a click while it
// restarts would freeze it on a blank screen.
func toApp(otherwise string) string {
	return `{ if -F "#{||:#{pane_in_mode},#{mouse_any_flag},#{==:#{@deckk_pane},dashboard}}" { send -M } { ` + otherwise + ` } }`
}

// Keys typed while scrolled back or selecting leave copy mode and reach the
// app, as in iTerm2. tmux can't pass on the key that arrived, so every key a
// terminal sends is bound by name. Escape only clears, so it can't interrupt
// Claude.
func copyModeBindings(table string) string {
	var b strings.Builder
	bind := func(key, command string) {
		fmt.Fprintf(&b, "bind -T %s %s %s\n", table, key, command)
	}
	bind("MouseDown1Pane", `{ `+noteClick+` ; select-pane ; if -F "#{scroll_position}" { send -X clear-selection } { send -X cancel } }`)
	bind("MouseDragEnd1Pane", "{ "+outside("other-end", "set-mark", "other-end", "stop-selection")+" }")
	bind("DoubleClick1Pane", sameClick(`{ select-pane ; send -X select-word ; `+outside("other-end", "set-mark", "other-end", "stop-selection")+` }`))
	bind("TripleClick1Pane", sameClick(`{ select-pane ; send -X select-line ; `+outside("other-end", "set-mark", "other-end", "stop-selection")+` }`))
	// Anchored at the mark the drag left, since tmux re-anchors at the cursor,
	// which moves when scrolling.
	bind("S-MouseDown1Pane", `{ select-pane ; if -F "#{selection_present}" { send -X clear-selection ; `+
		outside("jump-to-mark", "begin-selection", "jump-to-mark", "stop-selection")+` } }`)
	bind("C-c", `{ if -F "#{selection_present}" `+copied+` { send -X cancel ; send-keys C-c } }`)
	bind("MouseDown3Pane", `{ select-pane ; `+rightMenu+` }`)
	bind("User0", copyIfSelected)
	bind("User1", `{ `+findPrompt+` }`)
	bind("User2", `{ send -X search-again }`)
	bind("Escape", `{ send -X cancel }`)
	// A paste arrives as a key no binding can name, so it reaches Any, as do
	// mouse events nothing else claims; those have a position and are ignored.
	bind("Any", `{ if -F "#{!=:#{mouse_y},}" { set -gq @deckk_noop 1 } { send -X cancel } }`)
	for c := '!'; c <= '~'; c++ {
		key := quote(string(c))
		bind(key, `{ send -X cancel ; send-keys -l `+key+` }`)
		bind(quote("M-"+string(c)), `{ send -X cancel ; send-keys `+quote("M-"+string(c))+` }`)
	}
	for _, key := range passedKeys() {
		bind(key, `{ send -X cancel ; send-keys `+key+` }`)
	}
	return b.String()
}

func passedKeys() []string {
	var keys []string
	for c := 'a'; c <= 'z'; c++ {
		if !strings.ContainsRune("cim", c) {
			keys = append(keys, "C-"+string(c))
		}
	}
	named := []string{"Space", "Enter", "BSpace", "Tab", "BTab", "Up", "Down", "Left", "Right", "Home", "End", "PPage", "NPage", "IC", "DC"}
	for n := 1; n <= 12; n++ {
		named = append(named, fmt.Sprintf("F%d", n))
	}
	reserved := map[string]bool{"C-Space": true, "C-S-Up": true, "C-S-Down": true, "C-S-Left": true, "C-S-Right": true, "C-PPage": true, "C-NPage": true}
	for _, mod := range []string{"", "S-", "C-", "M-", "C-S-", "M-S-", "C-M-"} {
		for _, key := range named {
			if !reserved[mod+key] && (mod == "" || key != "BTab") {
				keys = append(keys, mod+key)
			}
		}
	}
	return keys
}

// tmuxBin runs copy-mode commands from outside a mouse binding, since inside
// one tmux first moves the cursor to the pointer for every command.
var tmuxBin = func() string {
	if path, err := exec.LookPath("tmux"); err == nil {
		return path
	}
	return "tmux"
}()

func outside(commands ...string) string {
	sends := make([]string, len(commands))
	for i, c := range commands {
		sends[i] = "send -t '#{pane_id}' -X " + c
	}
	return `run -b "` + shellQuote(tmuxBin) + ` -S '#{socket_path}' ` + strings.Join(sends, ` \\; `) + `"`
}

// Single quotes, since tmux expands ~ even inside double quotes.
func quote(s string) string {
	if strings.Contains(s, "'") {
		return `"` + s + `"`
	}
	return "'" + s + "'"
}

func otherPane(key string) string {
	return `{ if -F "#{&&:#{@deckk_dashboard},#{e|>:#{window_panes},1}}" { select-pane -t :.+ } { send-keys ` + key + ` } }`
}

// tmux sends a double-click a moment after the second press, to whatever pane
// is showing by then. When that press opened another tab, it would start a
// selection there, so double- and triple-clicks only act on the pane that
// took the press.
const noteClick = `set -gF @deckk_click "#{pane_id}"`

func sameClick(command string) string {
	return `{ if -F "#{==:#{pane_id},#{@deckk_click}}" ` + command + ` }`
}

const findPrompt = `command-prompt -p "find" { send -X search-backward-text "%%" }`

// tmux takes the right click, so the terminal's own menu never shows; this
// offers the parts of it a session needs.
var rightMenu = `display-menu -O -x M -y M ` +
	`"#{?selection_present,,-}Copy" c { send -X copy-pipe-no-clear ; display-message -d 1200 "Copied" } ` +
	`Paste v { if -F "#{pane_in_mode}" { send -X cancel } ; run "pbpaste | ` + shellQuote(tmuxBin) + ` -S '#{socket_path}' load-buffer -b deckk-paste -" ; paste-buffer -p -d -b deckk-paste } ` +
	`"Find…" f { copy-mode ; ` + findPrompt + ` } ` +
	`"#{?pane_in_mode,,-}Scroll to bottom" b { send -X cancel }`
