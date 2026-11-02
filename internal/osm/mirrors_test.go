package osm

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// A warm build must not ask a mirror again when any mirror's answer is already cached: otherwise it waits out the timeout of
// the mirror that was down last time, and may take different data from another mirror than the previous build did.
func TestCachedMirrorAnswerIsUsedBeforeTheNetwork(t *testing.T) {
	var hitsA, hitsB atomic.Int32
	a := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hitsA.Add(1); http.Error(w, "busy", 504) }))
	defer a.Close()
	b := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hitsB.Add(1)
		w.Write([]byte(`{"elements":[{"type":"way","geometry":[{"lat":1,"lon":1},{"lat":2,"lon":2}]}]}`))
	}))
	defer b.Close()
	old := Mirrors
	Mirrors = []string{a.URL, b.URL}
	defer func() { Mirrors = old }()
	dir := t.TempDir()

	p1, _, err := FetchCoastline(dir, 0, 0, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if hitsA.Load() != 1 || hitsB.Load() != 1 {
		t.Fatalf("cold: want one request to each mirror, got A=%d B=%d", hitsA.Load(), hitsB.Load())
	}
	p2, _, err := FetchCoastline(dir, 0, 0, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if hitsA.Load() != 1 || hitsB.Load() != 1 {
		t.Errorf("warm: no mirror may be asked again, got A=%d B=%d", hitsA.Load(), hitsB.Load())
	}
	if p1.SHA256 != p2.SHA256 {
		t.Errorf("warm build took different data: %s vs %s", p1.SHA256, p2.SHA256)
	}
}
