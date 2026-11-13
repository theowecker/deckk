package hq

import (
	"encoding/json"
	"errors"
	"io/fs"
	"maps"
	"os"
	"slices"
	"time"
)

// A Proposal is a new session the deckk session thinks the user should
// start: a folder and the first prompt to type into it.
type Proposal struct {
	ID     string    `json:"id"`
	Folder string    `json:"folder"`
	Prompt string    `json:"prompt"`
	Why    string    `json:"why,omitempty"`
	At     time.Time `json:"at"`
}

// Proposals go stale; work nobody started in a few days was not next.
const proposalsFor = 3 * 24 * time.Hour

// LoadProposals returns the live proposals, newest first.
func LoadProposals() ([]Proposal, error) {
	all, err := readProposals()
	if err != nil {
		return nil, err
	}
	list := slices.Collect(maps.Values(all))
	list = slices.DeleteFunc(list, func(p Proposal) bool { return time.Since(p.At) > proposalsFor })
	slices.SortFunc(list, func(a, b Proposal) int { return b.At.Compare(a.At) })
	return list, nil
}

func readProposals() (map[string]Proposal, error) {
	all := map[string]Proposal{}
	data, err := os.ReadFile(path("proposals.json"))
	if errors.Is(err, fs.ErrNotExist) {
		return all, nil
	} else if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &all); err != nil {
		return nil, err
	}
	return all, nil
}

func Propose(p Proposal) error {
	return changeProposals(func(all map[string]Proposal) { all[p.ID] = p })
}

func Withdraw(id string) error {
	return changeProposals(func(all map[string]Proposal) { delete(all, id) })
}

func changeProposals(change func(map[string]Proposal)) error {
	return locked(func() error {
		all, err := readProposals()
		if err != nil {
			return err
		}
		change(all)
		maps.DeleteFunc(all, func(_ string, p Proposal) bool { return time.Since(p.At) > proposalsFor })
		data, err := json.MarshalIndent(all, "", "  ")
		if err != nil {
			return err
		}
		return writeAtomic(path("proposals.json"), data)
	})
}
