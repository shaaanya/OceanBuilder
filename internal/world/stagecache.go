package world

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Stage cache. A stage is a pure function of its key: the key hashes everything the stage's output depends on (the
// content hashes of the upstream stages, the configuration it reads, algorithm and tool versions), so equal keys mean
// equal bytes and a changed input invalidates exactly the stages downstream of it.
//
// Two artifact shapes:
//   - a directory (compose, LOD layers): produced in a sibling *.tmp-* directory, sealed with STAGE.json (sha256 and
//     size of every file) and renamed into place in one step;
//   - a single file (warp, vertical shift): renamed into place first, then sealed with a <file>.seal sidecar.
//
// Consequences: an interrupted or failed stage leaves only temporary files or an unsealed file, never something that
// looks finished; on reuse every byte is hashed against the seal, so a truncated, edited or bit-rotted artifact is
// detected, reported, discarded and rebuilt rather than trusted; two builds sharing a cache cannot corrupt each other.
//
// Cached: source downloads (internal/fetch), vertical shifts, warps, the composed level-0 layer, every LOD level's
// layer. Not cached, always recomputed: the whole-grid steps (regional fill, coast conform, refinement), tile files and
// PNGs, the manifest. See docs/resume.md.

const stageSeal = "STAGE.json"

// errNoSeal: the file has no seal yet. It may be another build's artifact between rename and seal, so it is never
// deleted for this reason; the rebuild replaces it atomically.
var errNoSeal = errors.New("no seal (interrupted write, or another build is sealing it)")

// StageHook, when set (tests), is called at the points where a build can be interrupted:
// "start:<stage>" before the stage is looked up, "mid:<stage>" after its data is written but before it is sealed,
// "done:<stage>" after it was committed. A non-nil result aborts the build with that error, as a crash or Ctrl-C would.
var StageHook func(point string) error

func hook(point string) error {
	if StageHook != nil {
		return StageHook(point)
	}
	return nil
}

type sealedFile struct {
	SHA256 string `json:"sha256"`
	Bytes  int64  `json:"bytes"`
}

type stageSealDoc struct {
	Stage string                `json:"stage"`
	Key   string                `json:"key"`
	Files map[string]sealedFile `json:"files"`
}

func stagesDir(cacheDir string) string { return filepath.Join(cacheDir, "stages") }

// stageKey hashes the parts (in order) into a hex key.
func stageKey(parts ...any) string {
	h := sha256.New()
	for _, p := range parts {
		fmt.Fprintf(h, "%v\x00", p)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func hashFile(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	return hex.EncodeToString(h.Sum(nil)), n, err
}

// verifyStage checks dir against its seal for key; the error says what is wrong.
func verifyStage(dir, name, key string) error {
	b, err := os.ReadFile(filepath.Join(dir, stageSeal))
	if err != nil {
		return err
	}
	var s stageSealDoc
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("unreadable seal: %w", err)
	}
	if s.Key != key || s.Stage != name {
		return fmt.Errorf("seal is for stage %s key %.12s, want %s %.12s", s.Stage, s.Key, name, key)
	}
	for f, want := range s.Files {
		sum, n, err := hashFile(filepath.Join(dir, f))
		if err != nil {
			return err
		}
		if n != want.Bytes || sum != want.SHA256 {
			return fmt.Errorf("%s changed (size %d, sha256 %.12s; sealed %d, %.12s)", f, n, sum, want.Bytes, want.SHA256)
		}
	}
	return nil
}

// stage returns the verified directory holding the stage's files, producing them with produce when there is none (or
// the existing one fails verification). hit reports whether an existing artifact was reused.
func (b *bctx) stage(name, key string, produce func(dir string) error) (dir string, hit bool, err error) {
	root := stagesDir(b.opt.CacheDir)
	dir = filepath.Join(root, name+"-"+key[:24])
	if err := hook("start:" + name); err != nil {
		return "", false, err
	}
	if _, serr := os.Stat(dir); serr == nil {
		verr := verifyStage(dir, name, key)
		if verr == nil {
			b.opt.logf("stage %s: reusing verified artifact", name)
			return dir, true, nil
		}
		b.opt.logf("WARNING: stage %s: cached artifact %s failed verification (%v); rebuilding", name, filepath.Base(dir), verr)
		if err := os.RemoveAll(dir); err != nil {
			return "", false, err
		}
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", false, err
	}
	tmp, err := os.MkdirTemp(root, name+"-"+key[:24]+".tmp-")
	if err != nil {
		return "", false, err
	}
	committed := false
	defer func() {
		if !committed {
			os.RemoveAll(tmp) // never leave a half-written artifact where it could be mistaken for a finished one
		}
	}()
	if err := produce(tmp); err != nil {
		return "", false, err
	}
	if err := hook("mid:" + name); err != nil {
		return "", false, err
	}
	seal := stageSealDoc{Stage: name, Key: key, Files: map[string]sealedFile{}}
	ents, err := os.ReadDir(tmp)
	if err != nil {
		return "", false, err
	}
	for _, e := range ents {
		if e.IsDir() || e.Name() == stageSeal {
			continue
		}
		sum, n, err := hashFile(filepath.Join(tmp, e.Name()))
		if err != nil {
			return "", false, err
		}
		seal.Files[e.Name()] = sealedFile{SHA256: sum, Bytes: n}
	}
	if err := writeJSON(filepath.Join(tmp, stageSeal), seal); err != nil {
		return "", false, err
	}
	if err := os.Rename(tmp, dir); err != nil {
		// Another build may have committed the same key first: use theirs if it verifies.
		if verifyStage(dir, name, key) == nil {
			return dir, true, nil
		}
		return "", false, err
	}
	committed = true
	if err := hook("done:" + name); err != nil {
		return "", false, err
	}
	return dir, false, nil
}

// sealFile records size and sha256 of a single-file artifact in <path>.seal (written after the file is in place) and returns the sha256.
func sealFile(path string) (string, error) {
	sum, n, err := hashFile(path)
	if err != nil {
		return "", err
	}
	b, _ := json.Marshal(sealedFile{SHA256: sum, Bytes: n})
	// A unique temporary name: builds sharing a cache may seal the same artifact at the same time.
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".seal.tmp-")
	if err != nil {
		return "", err
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return "", err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return "", err
	}
	if err := os.Rename(tmp.Name(), path+".seal"); err != nil {
		os.Remove(tmp.Name())
		return "", err
	}
	return sum, nil
}

// verifySealed checks a single-file artifact against its seal and returns its sha256.
func verifySealed(path string) (string, error) {
	b, err := os.ReadFile(path + ".seal")
	if err != nil {
		return "", errNoSeal
	}
	var s sealedFile
	if err := json.Unmarshal(b, &s); err != nil {
		return "", fmt.Errorf("unreadable seal: %w", err)
	}
	sum, n, err := hashFile(path)
	if err != nil {
		return "", err
	}
	if n != s.Bytes || sum != s.SHA256 {
		return "", fmt.Errorf("size %d / sha256 %.12s differ from the seal (%d / %.12s)", n, sum, s.Bytes, s.SHA256)
	}
	return sum, nil
}

// reuseSealed returns the verified sha256 of an existing single-file artifact, or "" when there is none or it was
// discarded because it failed verification.
func (b *bctx) reuseSealed(path, what string) string {
	if _, err := os.Stat(path); err != nil {
		return ""
	}
	sum, err := verifySealed(path)
	if err == nil {
		return sum
	}
	b.opt.logf("WARNING: stage %s: cached file %s failed verification (%v); rebuilding", what, filepath.Base(path), err)
	if !errors.Is(err, errNoSeal) {
		os.Remove(path)
		os.Remove(path + ".seal")
	}
	return ""
}

// layerHash is a content identity for a layer written outside the stage cache (after the whole-grid steps).
func layerHash(l *layerFiles) (string, error) {
	h := sha256.New()
	for _, p := range []string{l.demPath, l.kindPath, l.provPath} {
		sum, _, err := hashFile(p)
		if err != nil {
			return "", err
		}
		fmt.Fprint(h, sum, "\x00")
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// sweepStages removes temporary stage directories and work directories that a crashed or killed build left behind.
// Only those older than two hours go: a running build in another process may own a younger one.
func sweepStages(cacheDir string) {
	old := func(e os.DirEntry) bool {
		fi, err := e.Info()
		return err == nil && time.Since(fi.ModTime()) > 2*time.Hour
	}
	ents, _ := os.ReadDir(stagesDir(cacheDir))
	for _, e := range ents {
		if e.IsDir() && strings.Contains(e.Name(), ".tmp-") && old(e) {
			os.RemoveAll(filepath.Join(stagesDir(cacheDir), e.Name()))
		}
	}
	ents, _ = os.ReadDir(cacheDir)
	for _, e := range ents {
		if e.IsDir() && strings.HasPrefix(e.Name(), "work-") && old(e) {
			os.RemoveAll(filepath.Join(cacheDir, e.Name()))
		}
	}
}
