package hq

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"regexp"
	"strings"
)

var goalLine = regexp.MustCompile(`^(?:#{1,3}\s+|[-*]\s+(?:\[[ xX]\]\s+)?)(.+)$`)

// GoalsIn lists the goals in the goals file: its headings and its top-level
// bullets, without their markdown.
func GoalsIn(text string) []string {
	var goals []string
	for line := range strings.Lines(text) {
		m := goalLine.FindStringSubmatch(strings.TrimRight(line, "\n"))
		if m == nil {
			continue
		}
		if goal := strings.TrimSpace(strings.NewReplacer("**", "", "__", "", "`", "").Replace(m[1])); goal != "" {
			goals = append(goals, goal)
		}
	}
	return goals
}

// MatchGoal finds the goal a name refers to: the same text whatever its case,
// or the one goal that contains it.
func MatchGoal(goals []string, name string) (string, error) {
	name = strings.TrimSpace(name)
	var containing []string
	for _, g := range goals {
		if strings.EqualFold(g, name) {
			return g, nil
		}
		if strings.Contains(strings.ToLower(g), strings.ToLower(name)) {
			containing = append(containing, g)
		}
	}
	if len(containing) == 1 {
		return containing[0], nil
	}
	if len(goals) == 0 {
		return "", errors.New("there are no goals yet; add them to goals.md first")
	}
	return "", fmt.Errorf("%q matches %d goals; use one of: %s", name, len(containing), strings.Join(goals, "; "))
}

// LoadLinks says which goal each session works toward, by session ID.
func LoadLinks() (map[string]string, error) {
	links := map[string]string{}
	data, err := os.ReadFile(path("links.json"))
	if errors.Is(err, fs.ErrNotExist) {
		return links, nil
	} else if err != nil {
		return nil, err
	}
	return links, json.Unmarshal(data, &links)
}

// LinkGoal ties a session to a goal, or unties it when goal is empty.
func LinkGoal(id, goal string) error {
	return locked(func() error {
		links, err := LoadLinks()
		if err != nil {
			return err
		}
		if goal == "" {
			delete(links, id)
		} else {
			links[id] = goal
		}
		data, err := json.MarshalIndent(links, "", "  ")
		if err != nil {
			return err
		}
		return writeAtomic(path("links.json"), data)
	})
}
