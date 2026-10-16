package lifecycle

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// ExitRecord is what the reaper writes when a process slot ends.
// State is exited, signaled, or stopped.
type ExitRecord struct {
	State  string    `json:"state"`
	Code   int       `json:"code,omitempty"`
	Signal string    `json:"signal,omitempty"`
	At     time.Time `json:"at"`
}

func writeExit(path string, rec ExitRecord) error {
	if rec.At.IsZero() {
		rec.At = time.Now()
	}
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, ".exit-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	encErr := json.NewEncoder(f).Encode(rec)
	closeErr := f.Close()
	if encErr != nil {
		_ = os.Remove(tmp)
		return encErr
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return closeErr
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func readExit(path string) (ExitRecord, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ExitRecord{}, err
	}
	var rec ExitRecord
	if err := json.Unmarshal(raw, &rec); err != nil {
		return ExitRecord{}, err
	}
	return rec, nil
}
