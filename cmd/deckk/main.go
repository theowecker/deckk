package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/theowecker/deckk/internal/claude"
	"github.com/theowecker/deckk/internal/config"
	"github.com/theowecker/deckk/internal/deck"
	"github.com/theowecker/deckk/internal/hq"
	"github.com/theowecker/deckk/internal/mcp"
	"github.com/theowecker/deckk/internal/store"
	"github.com/theowecker/deckk/internal/terminal"
	"github.com/theowecker/deckk/internal/tmux"
	"github.com/theowecker/deckk/internal/ui"
)

var version = "dev"

const usage = `deckk keeps all your Claude Code sessions in one tab.

Usage:
  deckk [--days N]    open deckk, or reattach if it is already running
  deckk ls            list sessions
  deckk stop          close deckk and every session running inside it
  deckk version

Inside deckk, Ctrl+Space switches between the dashboard and your last session.
`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "deckk:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	cmd := ""
	if len(args) > 0 && args[0] != "" && args[0][0] != '-' {
		cmd, args = args[0], args[1:]
	}

	flags := flag.NewFlagSet("deckk", flag.ContinueOnError)
	flags.Usage = func() { fmt.Fprint(os.Stderr, usage) }
	cfg, err := config.Load(config.DefaultPath())
	if err != nil {
		return err
	}
	days := flags.Int("days", cfg.Days, "show sessions active in the last N days")
	program := flags.String("terminal", "", "the $TERM_PROGRAM deckk was started from")
	role := flags.String("role", hq.RoleChild, "which tools deckk mcp serves")
	if err := flags.Parse(args); err != nil {
		return err
	}

	switch cmd {
	case "":
		return launch(*days, cfg)
	case "dashboard":
		return dashboard(*days, cfg, *program)
	case "ls":
		return list(*days)
	case "mcp":
		return mcp.Serve(os.Stdin, os.Stdout, "deckk", version, hq.Tools(*role))
	case "stop":
		return tmux.Kill()
	case "version":
		fmt.Println(version)
		return nil
	case "help":
		fmt.Print(usage)
		return nil
	default:
		return fmt.Errorf("unknown command %q\n\n%s", cmd, usage)
	}
}

func launch(days int, cfg *config.Config) error {
	tmuxPath, err := tmux.Path()
	if err != nil {
		return err
	}
	if tmux.Inside() {
		return tmux.Select(":0")
	}

	exe, err := os.Executable()
	if err != nil {
		return err
	}
	app, _ := terminal.Current()
	dashboardCmd := shellQuote(exe) + " dashboard --days " + strconv.Itoa(days) + " --terminal " + shellQuote(app.Program())

	tmuxConf, err := tmux.WriteConfig()
	if err != nil {
		return err
	}
	if tmux.HasSession() {
		if err := tmux.Reload(tmuxConf); err != nil {
			return err
		}
		if err := tmux.RestartDashboard(dashboardCmd); err != nil {
			return err
		}
	} else if err := tmux.Start(tmuxConf, dashboardCmd); err != nil {
		return err
	}

	if err := tmux.BindTabKeys(cfg.TabKeys); err != nil {
		return err
	}
	defer app.Setup(os.Stdout, cfg.TabKeys)()
	return tmux.Attach(tmuxPath)
}

func dashboard(days int, cfg *config.Config, program string) error {
	if !tmux.Inside() {
		return errors.New("the dashboard runs inside deckk; start it with plain deckk")
	}
	st, err := store.Open(store.DefaultPath())
	if err != nil {
		return err
	}
	tmuxPath, err := tmux.Path()
	if err != nil {
		return err
	}
	model := ui.New(claude.Home(), since(days), st, cfg, newHost(tmuxPath, program, cfg))
	_, err = tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseAllMotion(), tea.WithReportFocus()).Run()
	return err
}

func list(days int) error {
	st, err := store.Open(store.DefaultPath())
	if err != nil {
		return err
	}
	sessions, err := claude.Load(claude.Home(), since(days), st.Pinned)
	if err != nil {
		return err
	}
	st.ApplyNames(sessions)
	sessions = slices.DeleteFunc(sessions, func(s claude.Session) bool { return hq.Is(s.Cwd) })
	marks := deck.Marks{Pinned: st.Pinned, Archived: st.Archived}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	now := time.Now()
	for _, g := range deck.Arrange(sessions, marks, "", false) {
		for _, s := range g.Sessions {
			state := deck.Ago(s.UpdatedAt, now)
			if s.Running() {
				state = s.Status
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", claude.ShortID(s.ID), g.Section, s.Project(), s.Title, state)
		}
	}
	return w.Flush()
}

// tmux runs window commands through sh, so the path is single-quoted.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func since(days int) time.Time {
	return time.Now().AddDate(0, 0, -days)
}
