package fetch

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Lock pins the exact bytes of every source a world was built from, so a rebuild after cache
// eviction or an upstream change fails instead of silently producing different output.
type Lock struct {
	Sources map[string]LockEntry `json:"sources"` // key: LockKey
}

type LockEntry struct {
	SHA256      string `json:"sha256"`
	Bytes       int64  `json:"bytes"`
	RetrievedAt string `json:"retrieved_at"`
}

// LockKey identifies a source request. Local files are keyed by base name so a lock is portable.
func LockKey(p Provenance) string {
	if strings.HasPrefix(p.URL, "file://") {
		return "file://" + filepath.Base(strings.TrimPrefix(p.URL, "file://"))
	}
	return p.URL
}

// NewLock records the given sources.
func NewLock(ps []Provenance) *Lock {
	l := &Lock{Sources: map[string]LockEntry{}}
	for _, p := range ps {
		l.Sources[LockKey(p)] = LockEntry{p.SHA256, p.Bytes, p.RetrievedAt}
	}
	return l
}

// ReadLock returns (nil, nil) when the file does not exist.
func ReadLock(path string) (*Lock, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var l Lock
	if err := json.Unmarshal(b, &l); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &l, nil
}

func (l *Lock) Write(path string) error {
	b, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

// Check returns one message per difference between the lock and the sources a build resolved:
// changed content, sources the lock does not know, and locked sources the build no longer uses.
func (l *Lock) Check(ps []Provenance) []string {
	var bad []string
	seen := map[string]bool{}
	for _, p := range ps {
		k := LockKey(p)
		seen[k] = true
		e, ok := l.Sources[k]
		switch {
		case !ok:
			bad = append(bad, "not in lock: "+k)
		case e.SHA256 != p.SHA256:
			bad = append(bad, fmt.Sprintf("content changed: %s (locked sha256 %.12s, now %.12s)", k, e.SHA256, p.SHA256))
		}
	}
	for k := range l.Sources {
		if !seen[k] {
			bad = append(bad, "locked but unused: "+k)
		}
	}
	sort.Strings(bad)
	return bad
}
