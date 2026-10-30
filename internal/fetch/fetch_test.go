package fetch

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestCacheHitIsVerifiedAndCorruptionRefetches(t *testing.T) {
	body, hits := "v1", 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits++; w.Write([]byte(body)) }))
	defer srv.Close()
	dir := t.TempDir()
	p, err := Get(dir, "s", srv.URL, nil, "f.bin", "lic", "attr")
	if err != nil || hits != 1 {
		t.Fatal(err, hits)
	}
	if _, err := Get(dir, "s", srv.URL, nil, "f.bin", "lic", "attr"); err != nil || hits != 1 {
		t.Fatalf("intact cache must be reused: err=%v hits=%d", err, hits)
	}
	body = "v2" // upstream changed and the cached file is corrupted
	os.WriteFile(p.File, []byte("garbage"), 0o644)
	p2, err := Get(dir, "s", srv.URL, nil, "f.bin", "lic", "attr")
	if err != nil || hits != 2 || p2.SHA256 == p.SHA256 {
		t.Fatalf("corrupt cache must refetch: err=%v hits=%d", err, hits)
	}
}

func TestLockCheck(t *testing.T) {
	a := Provenance{URL: "http://x/a", SHA256: "aa"}
	b := Provenance{URL: "file:///home/me/b.tif", SHA256: "bb"}
	l := NewLock([]Provenance{a, b})
	if bad := l.Check([]Provenance{a, {URL: "file:///elsewhere/b.tif", SHA256: "bb"}}); len(bad) != 0 {
		t.Fatalf("local files are keyed by base name: %v", bad)
	}
	bad := strings.Join(l.Check([]Provenance{{URL: "http://x/a", SHA256: "zz"}, {URL: "http://x/c", SHA256: "cc"}}), "|")
	for _, want := range []string{"content changed: http://x/a", "not in lock: http://x/c", "locked but unused: file://b.tif"} {
		if !strings.Contains(bad, want) {
			t.Errorf("missing %q in %q", want, bad)
		}
	}
	path := t.TempDir() + "/l.json"
	if err := l.Write(path); err != nil {
		t.Fatal(err)
	}
	if got, err := ReadLock(path); err != nil || len(got.Sources) != 2 {
		t.Fatal(got, err)
	}
	if got, err := ReadLock(path + ".none"); got != nil || err != nil {
		t.Fatal("missing lock is (nil, nil)")
	}
}
