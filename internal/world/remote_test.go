package world_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/shaaanya/OceanBuilder/internal/world"
)

// A local source given as an http(s) URL is downloaded into the cache with provenance and gives the same
// world as the same files on disk (this is how a single remote tile, e.g. a NOAA CUDEM GeoTIFF, is used).
func TestRemoteURLSourceMatchesLocalFiles(t *testing.T) {
	srv := httptest.NewServer(http.FileServer(http.Dir("../../testdata/falmouth")))
	defer srv.Close()

	local := cfgFor(box)
	dirL := t.TempDir()
	mL, err := world.Build(local, world.Options{CacheDir: t.TempDir(), OutDir: dirL})
	if err != nil {
		t.Fatal(err)
	}

	remote := cfgFor(box)
	remote.Local.Topo, remote.Local.Mask, remote.Local.Coastline = srv.URL+"/topo.tif", srv.URL+"/mask.tif", srv.URL+"/coast.geojson"
	dirR, cache := t.TempDir(), t.TempDir()
	mR, err := world.Build(remote, world.Options{CacheDir: cache, OutDir: dirR})
	if err != nil {
		t.Fatal(err)
	}

	if len(mR.Tiles) != len(mL.Tiles) {
		t.Fatalf("%d tiles from URLs, %d from files", len(mR.Tiles), len(mL.Tiles))
	}
	a, b := readAll(t, dirL, mL), readAll(t, dirR, mR)
	for k, v := range a {
		if k == world.ManifestFile || k == "ATTRIBUTION.txt" {
			continue // provenance (URL, retrieval time) legitimately differs
		}
		if !bytes.Equal(v, b[k]) {
			t.Fatalf("%s differs between file and URL source", k)
		}
	}
	for _, s := range mR.Sources {
		if !strings.HasPrefix(s.URL, srv.URL) || len(s.SHA256) != 64 || s.RetrievedAt == "" || s.License == "" {
			t.Fatalf("remote source lacks provenance: %+v", s)
		}
	}

	// a second build reuses the cached download: the server may be gone
	srv.Close()
	if _, err := world.Build(remote, world.Options{CacheDir: cache, OutDir: t.TempDir()}); err != nil {
		t.Fatalf("rebuild from cache after the server went away: %v", err)
	}
}
