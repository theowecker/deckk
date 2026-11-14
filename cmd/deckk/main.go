package main

import (
	"bufio"
	"cmp"
	"errors"
	"flag"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"text/tabwriter"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/charmbracelet/x/term"

	"github.com/theowecker/deckk/internal/claude"
	"github.com/theowecker/deckk/internal/config"
	"github.com/theowecker/deckk/internal/deck"
	"github.com/theowecker/deckk/internal/hq"
	"github.com/theowecker/deckk/internal/mcp"
	"github.com/theowecker/deckk/internal/store"
	"github.com/theowecker/deckk/internal/terminal"
	"github.com/theowecker/deckk/internal/tmux"
	"github.com/theowecker/deckk/internal/ui"
	"github.com/theowecker/deckk/internal/web"
)

var version = "dev"

const usage = `deckk keeps all your Claude Code sessions in one tab.

Usage:
  deckk [--days N]    open deckk, or reattach if it is already running
  deckk ls            list sessions
  deckk stop          close deckk and every session running inside it
  deckk focus ID      switch to a running session, wherever its tab is
  deckk doctor        check what deckk needs and how to fix what's missing
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
	if len(args) == 1 && (args[0] == "--version" || args[0] == "-v") {
		cmd, args = "version", nil
	}

	flags := flag.NewFlagSet("deckk", flag.ContinueOnError)
	flags.Usage = func() { fmt.Fprint(os.Stderr, usage) }
	cfg, err := config.Load(config.DefaultPath())
	if cmd == "doctor" {
		if !doctor(os.Stdout, cfg, err) {
			os.Exit(1)
		}
		return nil
	}
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
	case "intro":
		if !hq.Intro(os.Stdin, os.Stdout, cfg) {
			os.Exit(1)
		}
		return nil
	case "mcp":
		return mcp.Serve(os.Stdin, os.Stdout, "deckk", version, hq.Tools(*role))
	case "stop":
		return stop()
	case "focus":
		return focus(flags.Arg(0))
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
	if !term.IsTerminal(os.Stdin.Fd()) || !term.IsTerminal(os.Stdout.Fd()) {
		return errors.New("deckk needs a terminal: run it in a terminal tab, not from a script or launcher that redirects its input or output")
	}
	if err := restartStaleServer(); err != nil {
		return err
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
	if bg, ok := terminal.Background(os.Stdin, os.Stdout, 300*time.Millisecond); ok {
		_ = tmux.SetBackground(bg)
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
	if err := tmux.MarkDashboard(os.Getenv("TMUX_PANE")); err != nil {
		return err
	}
	model := ui.New(claude.Home(), since(days), st, cfg, newHost(tmuxPath, program))
	// The page's actions reach the dashboard like key presses, once the
	// program that takes them exists.
	var running atomic.Pointer[tea.Program]
	tokenPath := filepath.Join(filepath.Dir(config.DefaultPath()), "web.token")
	model = model.WithWeb(func() (ui.Publisher, error) {
		return web.Start(tokenPath, func(a web.Action) error {
			p := running.Load()
			if p == nil {
				return web.ErrNotReady
			}
			p.Send(ui.WebAction(a))
			return nil
		}, history, search(st))
	})
	p := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseAllMotion(), tea.WithReportFocus())
	running.Store(p)
	_, err = p.Run()
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

const searchLimit = 80

func search(st *store.Store) web.Search {
	return func(query string) ([]web.Found, error) {
		labels := st.Labels()
		sessions, err := loadEverything(labels.Pinned)
		if err != nil {
			return nil, err
		}
		sessions = slices.Clone(sessions)
		labels.ApplyNames(sessions)
		notes, _ := hq.LoadNotes()
		score := func(s claude.Session) int { return deck.Score(s, query, notes[s.ID].Text) }
		var yours, unmatched, background []claude.Session
		for _, s := range sessions {
			switch {
			case hq.Is(s.Cwd):
			case score(s) == 0 && !s.Background && s.Path != "":
				unmatched = append(unmatched, s)
			case score(s) == 0:
			case s.Background:
				background = append(background, s)
			default:
				yours = append(yours, s)
			}
		}
		deck.Rank(yours, score)
		deck.Rank(background, score)
		said := mentioning(unmatched, query)
		deck.Rank(said, func(claude.Session) int { return 0 })

		found := make([]web.Found, 0, searchLimit)
		add := func(list []claude.Session, mentioned bool) {
			for _, s := range list[:min(len(list), searchLimit-len(found))] {
				found = append(found, web.Found{
					ID: s.ID, Title: cmp.Or(s.Title, "new session"), Project: s.Project(), Folder: s.Cwd, Branch: s.Branch,
					Updated: s.UpdatedAt, LastPrompt: clip(s.LastPrompt, 200), Running: s.Running(), Background: s.Background,
					Pinned: labels.Pinned[s.ID], Archived: labels.Archived[s.ID], Mentioned: mentioned,
				})
			}
		}
		add(yours, false)
		add(said, true)
		add(background, false)
		return found, nil
	}
}

// Typing a search asks once per pause, so a few seconds of reuse spares
// rereading every transcript, and running git in each session, per letter.
var everything struct {
	sync.Mutex
	at       time.Time
	sessions []claude.Session
}

func loadEverything(pinned map[string]bool) ([]claude.Session, error) {
	everything.Lock()
	defer everything.Unlock()
	if time.Since(everything.at) < 5*time.Second {
		return everything.sessions, nil
	}
	sessions, err := claude.Load(claude.Home(), time.Time{}, pinned)
	if err != nil {
		return nil, err
	}
	everything.at, everything.sessions = time.Now(), sessions
	return sessions, nil
}

func history(id string) ([]web.Turn, error) {
	if !claude.IsSessionID(id) {
		return nil, errors.New("no such session")
	}
	path := claude.TranscriptPath(claude.Home(), id)
	if path == "" {
		return nil, errors.New("no such session")
	}
	exchanges, err := claude.Exchanges(path, 10)
	if err != nil {
		return nil, err
	}
	turns := make([]web.Turn, len(exchanges))
	for i, e := range exchanges {
		turns[i] = web.Turn{Prompt: clip(e.Prompt, 1200), Reply: clip(e.Reply, 2400)}
	}
	return turns, nil
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

// tmux runs window commands through sh, so the path is single-quoted.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func since(days int) time.Time {
	return time.Now().AddDate(0, 0, -days)
}

func restartStaleServer() error {
	if !tmux.HasSession() {
		return nil
	}
	server, installed, stale := tmux.Stale()
	if !stale {
		return nil
	}
	fmt.Printf("deckk's tmux server still runs tmux %s, but tmux %s is installed now, and the two can't talk.\n", server, installed)
	fmt.Println("Restarting the server closes the Claude sessions inside deckk. They stay resumable, and R on the dashboard reopens them.")
	fmt.Print("Restart it now? [y/N] ")
	answer, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	if !strings.EqualFold(strings.TrimSpace(answer), "y") {
		return errors.New("deckk can't attach until its tmux server restarts; run deckk again and answer y")
	}
	return tmux.KillServer()
}

func mentioning(sessions []claude.Session, query string) []claude.Session {
	if len(strings.TrimSpace(query)) < 3 {
		return nil
	}
	paths := make([]string, len(sessions))
	for i, s := range sessions {
		paths[i] = s.Path
	}
	found, err := claude.Mentioning(paths, query)
	if err != nil {
		return nil
	}
	return slices.DeleteFunc(slices.Clone(sessions), func(s claude.Session) bool { return !found[s.Path] })
}

func focus(id string) error {
	if !claude.IsSessionID(id) {
		return errors.New("focus needs a session ID")
	}
	sessions, err := claude.Load(claude.Home(), time.Now().AddDate(0, 0, -30), nil)
	if err != nil {
		return err
	}
	i := slices.IndexFunc(sessions, func(s claude.Session) bool { return s.ID == id && s.Running() })
	if i < 0 {
		return errors.New("that session isn't running")
	}
	tty := sessions[i].TTY
	windows, _ := tmux.Windows()
	for _, w := range windows {
		if w.TTY != tty || tty == "" {
			continue
		}
		if w.Session == tmux.Session {
			if err := tmux.Select(w.ID); err != nil {
				return err
			}
		}
		if w.ClientTTY != "" {
			return terminal.Focus(w.ClientTTY)
		}
		return nil
	}
	return terminal.Focus(tty)
}

// Stopping closes every session on purpose, so none of them wait to reopen.
func stop() error {
	if err := tmux.Kill(); err != nil {
		return err
	}
	st, err := store.Open(store.DefaultPath())
	if err != nil {
		return err
	}
	return st.SetOpen(nil, slices.Collect(maps.Keys(st.Open)))
}
