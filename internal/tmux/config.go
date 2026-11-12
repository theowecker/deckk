package tmux

import (
	"fmt"
	"os"
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

// tmux owns the mouse, so iTerm2 never sees a selection and Cmd+C has nothing
// to copy. Cmd+C or Ctrl+C sends the selection to the macOS clipboard and,
// like iTerm2, leaves it highlighted.
const copied = `{ send -X copy-pipe-no-clear ; display-message -d 1200 "Copied" }`

const copyIfSelected = `{ if -F "#{selection_present}" ` + copied + ` }`

// F12 rather than n, so the picker opens even while the filter has focus.
const clickStatus = `{ if -F "#{==:#{mouse_status_range},new}" { select-window -t :0 ; send-keys -t :0 F12 } { if -F "#{==:#{mouse_status_range},info}" ` + toggleInfo + ` { select-window -t = } } }`

// The info band is the strip's last row, so hiding it is one row less.
const toggleInfo = `{ if -F "#{==:#{status},4}" { set -g status 3 } { set -g status 4 } }`

// The ▴ button collapses the info band and ▾ brings it back.
const infoButton = `#[range=user|info]#[bg=#30304A,fg=#8A8A9A]  #{?#{==:#{status},4},▴,▾}  #[norange]#[default] `

// Claude never resets the pointer the dashboard sets. Switches the dashboard
// makes have no client tty, so it resets the pointer itself before those.
const resetPointer = `{ if -F "#{client_tty}" { run-shell -b "printf '\\033]22;\\007' > '#{client_tty}' 2>/dev/null; true" } }`

var tmuxConf = `
set -g default-terminal "tmux-256color"
set -s terminal-features[90] "xterm*:RGB:extkeys:clipboard"
set -g extended-keys on
set -g escape-time 0
set -g focus-events on
set -g allow-passthrough on
set -g set-clipboard on
set -g history-limit 50000
set -g mouse on
# deckk never needs the prefix, and Claude uses Ctrl+B itself.
set -g prefix None
set -g prefix2 None
unbind C-b
set -g copy-command "pbcopy"
set -s user-keys[0] "` + strings.ReplaceAll(terminal.CopySequence, "\x1b", `\e`) + `"
bind -n User0 { if -F "#{pane_in_mode}" ` + copyIfSelected + ` }
bind -n DoubleClick1Pane ` + toApp(`select-pane -t = ; copy-mode -H ; send -X select-word`) + `
bind -n TripleClick1Pane ` + toApp(`select-pane -t = ; copy-mode -H ; send -X select-line`) + `
bind -n MouseDrag1Pane ` + toApp(`copy-mode -M`) + `
# deckk is one pane per window, so the mouse never splits, moves or resizes.
unbind -n C-MouseDrag1Pane
unbind -n M-MouseDrag1Pane
unbind -n MouseDrag1Border
unbind -n M-MouseDown3Pane
unbind -n MouseDown3StatusLeft
unbind -n M-MouseDown3Status
unbind -n MouseDown3Status
bind -n MouseDown3Pane { select-pane -t = ; send -M }
bind -n WheelUpPane ` + toApp(`if -F "#{alternate_on}" { send -M } { copy-mode -e }`) + `
` + copyModeBindings("copy-mode") + copyModeBindings("copy-mode-vi") + `set -g base-index 0
set -g renumber-windows on
set -g set-titles on
set -g set-titles-string "deckk"
setw -g allow-rename off
setw -g automatic-rename off
setw -g monitor-bell on

set -g status 4
set -g @deckk_cap_tab "#[fg=#30304A]#{@deckk_cap}"
set -g @deckk_cap_current "#[fg=#E8875B]#{@deckk_cap}"
set -g @deckk_base_tab "#[fg=#30304A]#{s/▄/▀/:@deckk_cap}"
set -g @deckk_base_current "#[fg=#E8875B]#{s/▄/▀/:@deckk_cap}"
set -g status-format[0] "` + strip("@deckk_cap_tab", "@deckk_cap_current", buttonCaps("▄")) + `"
set -g status-format[1] "` + strip("window-status-format", "window-status-current-format", "#{T:status-right}") + `"
set -g status-format[2] "` + strip("@deckk_base_tab", "@deckk_base_current", buttonCaps("▀")) + `"
set -g status-format[3] "#{?@deckk_info,#[fill=#24243A bg=#24243A]#{@deckk_info},}"
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
set -g window-status-format "#[bg=#30304A]  ` + glyph + `#{?#{==:#{@deckk_status},fresh},#[fg=#E8875B#,bold],#[fg=#B4B4C0]} ` + name + `  #[default]"
set -g window-status-current-format "#[bg=#E8875B,fg=#1E1E2E,bold]  ` + plainGlyph + ` ` + name + `  #[default]"
set -g message-style "bg=default,fg=#E8875B"
set -g pane-border-style "fg=#3A3A3A"
set -g pane-active-border-style "fg=#E8875B"

bind -n MouseDown1Status ` + clickStatus + `
bind -n MouseDown1StatusRight ` + clickStatus + `
bind -n C-Space if -F "#{!=:#{session_name},deckk}" "" { if -F "#{@deckk_dashboard}" "last-window" "select-window -t :0" }

set-hook -g session-window-changed ` + resetPointer + `
set-hook -g client-detached ` + resetPointer + `
# A tab never opens on a leftover selection.
set-hook -g client-attached { copy-mode -q }
`

// strip is one row of the tab strip. The label row sits between two rows of
// half blocks in each tab's color, which make the tabs look twice as tall.
// Every row lists the same windows with the same click ranges.
func strip(tab, current, right string) string {
	sep := "#{?loop_last_flag,,#{E:window-status-separator}}"
	return "#[align=left range=left]#{T:status-left}#[norange default]#[list=on align=left]" +
		"#{W:#[range=window|#{window_index}]#[push-default]#{T:" + tab + "}#[pop-default]#[norange default]" + sep +
		",#[range=window|#{window_index} list=focus]#[push-default]#{T:" + current + "}#[pop-default]#[norange list=on default]" + sep + "}" +
		"#[nolist align=right range=right]" + right + "#[norange default]"
}

// The buttons' caps, as wide as "  ▴  " and "  + new  ".
func buttonCaps(half string) string {
	return "#[range=user|info]#[fg=#30304A]" + strings.Repeat(half, 5) + "#[norange default] " +
		"#[range=user|new]#[fg=#30304A]" + strings.Repeat(half, 9) + "#[norange default] "
}

// Cap is a tab's top edge, as wide as its label in window-status-format.
func Cap(name string) string {
	return strings.Repeat("▄", ansi.StringWidth(strings.ReplaceAll(name, "##", "#"))+6)
}

// BindTabKeys sets the keys that move to the previous and next tab, from the
// tab_keys setting. Ctrl+Shift+arrows reach tmux from any terminal and
// nothing else uses them.
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
	path := filepath.Join(dir, "deckk", "tmux.conf")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	return path, os.WriteFile(path, []byte(tmuxConf), 0o644)
}

// The dashboard never enters copy mode. It has no history to scroll, and a
// click while it restarts would otherwise freeze it on a blank screen.
func toApp(otherwise string) string {
	return `{ if -F "#{||:#{pane_in_mode},#{mouse_any_flag},#{@deckk_dashboard}}" { send -M } { ` + otherwise + ` } }`
}

// Typing while text is selected leaves the selection and goes to the app, as
// it would in iTerm2. Escape only clears, so it can't interrupt Claude.
func copyModeBindings(table string) string {
	var b strings.Builder
	bind := func(key, command string) {
		fmt.Fprintf(&b, "bind -T %s %s %s\n", table, key, command)
	}
	bind("MouseDown1Pane", `{ select-pane ; if -F "#{scroll_position}" { send -X clear-selection } { send -X cancel } }`)
	bind("MouseDragEnd1Pane", `{ send -X stop-selection }`)
	bind("DoubleClick1Pane", `{ select-pane ; send -X select-word }`)
	bind("TripleClick1Pane", `{ select-pane ; send -X select-line }`)
	bind("C-c", `{ if -F "#{selection_present}" `+copied+` { send -X cancel ; send-keys C-c } }`)
	bind("Escape", `{ send -X cancel }`)
	for c := '!'; c <= '~'; c++ {
		key := quote(string(c))
		bind(key, `{ send -X cancel ; send-keys -l `+key+` }`)
	}
	for _, key := range []string{"Space", "Enter", "BSpace", "Tab", "BTab", "Up", "Down", "Left", "Right"} {
		bind(key, `{ send -X cancel ; send-keys `+key+` }`)
	}
	return b.String()
}

// Single quotes, since tmux expands ~ even inside double quotes.
func quote(s string) string {
	if s == "'" {
		return `"'"`
	}
	return "'" + s + "'"
}
