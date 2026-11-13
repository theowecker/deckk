package deck

import (
	"slices"
	"testing"

	"github.com/theowecker/deckk/internal/claude"
)

func TestCollisions(t *testing.T) {
	running := func(id, cwd string) claude.Session { return claude.Session{ID: id, Cwd: cwd, PID: 1} }
	sessions := []claude.Session{
		running("a", "/shop"), running("b", "/shop/web"), running("c", "/api"),
		running("d", "/shop-wt"), {ID: "e", Cwd: "/shop"},
	}
	edits := map[string][]string{
		"a": {"/shop/cart.go", "/shop/tax.go"},
		"d": {"/shop/cart.go"},
		"c": {"/api/x.go"},
		"e": {"/shop/cart.go"},
	}
	root := func(dir string) string {
		switch dir {
		case "/shop", "/shop/web":
			return "/shop"
		}
		return dir
	}
	got := Collisions(sessions, edits, root)
	if len(got) != 2 {
		t.Fatalf("got %v", got)
	}
	if !got[0].Clash() || got[0].A.ID != "a" || got[0].B.ID != "d" || !slices.Equal(got[0].Files, []string{"/shop/cart.go"}) {
		t.Fatalf("clash first: %+v", got[0])
	}
	if got[1].Clash() || got[1].Checkout != "/shop" || got[1].Other("a").ID != "b" {
		t.Fatalf("then the shared checkout: %+v", got[1])
	}
	if len(CollisionsOf(got, "c")) != 0 || len(CollisionsOf(got, "a")) != 2 {
		t.Fatal("c collides with nobody, a with two")
	}
}
