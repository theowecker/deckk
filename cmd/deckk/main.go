package main

import (
	"cmp"
	"errors"
	"flag"
	"fmt"
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

	"github.com/muesli/termenv"

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
	case "intro":
		if !hq.Intro(os.Stdin, os.Stdout, cfg) {
			os.Exit(1)
		}
		return nil
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
	if bg := termenv.NewOutput(os.Stdout).BackgroundColor(); bg != nil {
		if _, none := bg.(termenv.NoColor); !none {
			_ = tmux.SetBackground(termenv.ConvertToRGB(bg).Hex())
		}
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
		sessions = slices.DeleteFunc(sessions, func(s claude.Session) bool {
			return hq.Is(s.Cwd) || !deck.Matches(s, query)
		})
		slices.SortFunc(sessions, func(a, b claude.Session) int {
			if a.Background != b.Background {
				if a.Background {
					return 1
				}
				return -1
			}
			return b.UpdatedAt.Compare(a.UpdatedAt)
		})
		found := make([]web.Found, 0, min(len(sessions), searchLimit))
		for _, s := range sessions[:min(len(sessions), searchLimit)] {
			found = append(found, web.Found{
				ID: s.ID, Title: cmp.Or(s.Title, "new session"), Project: s.Project(), Folder: s.Cwd, Branch: s.Branch,
				Updated: s.UpdatedAt, LastPrompt: clip(s.LastPrompt, 200), Running: s.Running(), Background: s.Background,
				Pinned: labels.Pinned[s.ID], Archived: labels.Archived[s.ID],
			})
		}
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
