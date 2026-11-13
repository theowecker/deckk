package hq

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"time"
)

// A Note is the deckk session's own memory of a session: what it is for and
// where it stands, so it doesn't reread transcripts that haven't moved.
type Note struct {
	Text string    `json:"text"`
	At   time.Time `json:"at"`
}

func LoadNotes() (map[string]Note, error) {
	all := map[string]Note{}
	data, err := os.ReadFile(path("notes.json"))
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

func Remember(id, text string) error {
	return locked(func() error {
		all, err := LoadNotes()
		if err != nil {
			return err
		}
		if text == "" {
			delete(all, id)
		} else {
			all[id] = Note{Text: text, At: time.Now()}
		}
		data, err := json.MarshalIndent(all, "", "  ")
		if err != nil {
			return err
		}
		return writeAtomic(path("notes.json"), data)
	})
}
