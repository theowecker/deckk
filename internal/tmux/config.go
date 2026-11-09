package tmux

import (
	"os"
	"path/filepath"
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
// to copy. Copying to the macOS clipboard on release makes Cmd+V just work.
const copied = `{ send -X copy-pipe-and-cancel ; display-message -d 1200 "Copied" }`

// Window count at which the ⌃Space hint gives its room to the tabs. The
// dashboard sizes tabs in ui/actions.go tabFit with the same threshold.
const crowded = "6"

// F12 rather than n, so the picker opens even while the filter has focus.
const clickStatus = `{ if -F "#{==:#{mouse_status_range},new}" { select-window -t :0 ; send-keys -t :0 F12 } { select-window -t = } }`

// Claude never resets the pointer the dashboard sets. Switches the dashboard
// makes have no client tty, so it resets the pointer itself before those.
const resetPointer = `{ if -F "#{client_tty}" { run-shell -b "printf '\\033]22;\\007' > '#{client_tty}' 2>/dev/null; true" } }`

const config = `
set -g default-terminal "tmux-256color"
set -s terminal-features[90] "xterm*:RGB:extkeys:clipboard"
set -g extended-keys on
set -g escape-time 0
set -g focus-events on
set -g allow-passthrough on
set -g set-clipboard on
set -g history-limit 50000
set -g mouse on
set -g copy-command "pbcopy"
set -s user-keys[0] "\e[deckk-copy~"
bind -n User0 { if -F "#{pane_in_mode}" ` + copied + ` }
bind -T copy-mode MouseDragEnd1Pane ` + copied + `
bind -T copy-mode-vi MouseDragEnd1Pane ` + copied + `
set -g base-index 0
set -g renumber-windows on
set -g set-titles on
set -g set-titles-string "deckk"
setw -g allow-rename off
setw -g automatic-rename off
setw -g monitor-bell on

set -g status 2
set -g status-format[1] ""
set -g status-position top
set -g status-justify left
set -g status-style "bg=default,fg=default"
set -g status-left " "
set -g status-left-length 2
set -g status-right "#{?#{e|<:#{session_windows},` + crowded + `},#[fg=#6B6B6B]⌃Space #[fg=#4A4A4A]dashboard   ,}#[range=user|new]#[bg=#30304A,fg=#E8875B,bold]  + new  #[norange]#[default] "
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
`

func WriteConfig() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, "deckk", "tmux.conf")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	return path, os.WriteFile(path, []byte(config), 0o644)
}
