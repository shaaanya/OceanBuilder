// Package gebco fetches a subset of the GEBCO grid and its Type Identifier (TID) grid through the REST API behind
// https://download.gebco.net (the "Grid Subsetting App"). That API is not documented by GEBCO: it was found in the app's
// JavaScript and observed working on 2026-11-01 (docs/research/source-probes.md), and it can change. A failure names the
// step that failed. GEBCO also publishes the global grid as a single NetCDF file, which can be used as a local source.
package gebco

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/shaaanya/OceanBuilder/internal/fetch"
	"github.com/shaaanya/OceanBuilder/internal/gdal"
)

// APIURL is the base of the REST API; tests point it at a local server.
var APIURL = "https://download.gebco.net/api"

// Poll is the wait between status checks and Timeout bounds the whole request.
var (
	Poll    = 2 * time.Second
	Timeout = 5 * time.Minute
)

const (
	License     = "GEBCO Grid terms of use: public domain, free of charge; acknowledge the source; must not be used for navigation"
	Attribution = "GEBCO Bathymetric Compilation Group 2026 (2026). The GEBCO_2026 Grid - a continuous terrain model of the global oceans and land. NERC EDS British Oceanographic Data Centre (NOC). doi:10.5285/4f68d5c7-45eb-f999-e063-7086abc0ea6f"
	// MeasuredTIDMin..Max are the TID codes of direct measurements (singlebeam, multibeam, seismic, isolated sounding,
	// ENC sounding, lidar, optical, combination). Codes 40 and above are indirect (predicted from gravity, interpolated,
	// contours, ...) and codes 70+ are unknown/pre-generated, none of which is a measurement at output resolution.
	MeasuredTIDMin, MeasuredTIDMax = 10, 17
)

type basket struct {
	ID     string `json:"id"`
	Email  any    `json:"email"`
	Date   string `json:"submission_date"`
	Status string `json:"processing_status"`
	Items  []item `json:"items"`
}

type item struct {
	ID      int     `json:"id"`
	GridID  int     `json:"grid_id"`
	Sources []int   `json:"data_source_ids"`
	Formats []int   `json:"formats"`
	Left    float64 `json:"left"`
	Right   float64 `json:"right"`
	Top     float64 `json:"top"`
	Bottom  float64 `json:"bottom"`
}

// Fetch returns the elevation GeoTIFF subset and a survey mask derived from the TID grid (1 where TID says direct
// measurement, nodata elsewhere). Grid id 1 and data sources 1 (bathymetry) and 3 (TID) are those the API listed on
// 2026-11-01 for GEBCO_2026.
func Fetch(cacheDir string, w, s, e, n float64) (elev, mask *fetch.Provenance, err error) {
	key := fmt.Sprintf("gebco|grid1|%.6f,%.6f,%.6f,%.6f", w, s, e, n)
	desc := fmt.Sprintf("%s (GEBCO_2026 grid_id 1, subset w=%.5f s=%.5f e=%.5f n=%.5f)", APIURL, w, s, e, n)
	ps, err := fetch.Derived(cacheDir, "gebco", key, desc, License, Attribution, []string{"gebco.tif", "gebco-tid.tif", "gebco-measured-mask.tif"}, func(dir string) error {
		zipBytes, err := download(w, s, e, n)
		if err != nil {
			return err
		}
		if err := extract(zipBytes, dir); err != nil {
			return err
		}
		// mask: 1 where the TID code is a direct measurement, otherwise nodata (same georeferencing as the TID grid)
		_, err = gdal.Run("gdal_calc.py", "-A", filepath.Join(dir, "gebco-tid.tif"), "--outfile="+filepath.Join(dir, "gebco-measured-mask.tif"),
			fmt.Sprintf("--calc=where((A>=%d)&(A<=%d),1,0)", MeasuredTIDMin, MeasuredTIDMax), "--NoDataValue=0", "--type=Float32", "--quiet", "--overwrite")
		return err
	})
	if err != nil {
		return nil, nil, err
	}
	// the TID grid is provenance of the mask; the mask record says what it was derived from
	ps[2].URL = ps[2].URL + " [mask derived from the TID layer: codes 10-17 = direct measurement]"
	ps[2].Source = "gebco-measured-mask"
	return ps[0], ps[2], nil
}

func download(w, s, e, n float64) ([]byte, error) {
	client := &http.Client{Timeout: 60 * time.Second}
	deadline := time.Now().Add(Timeout)
	body, _ := json.Marshal(basket{ID: "0", Date: time.Now().UTC().Format("2006-01-02T15:04:05.000000"), Status: "new",
		Items: []item{{GridID: 1, Sources: []int{1, 3}, Formats: []int{2}, Left: w, Right: e, Top: n, Bottom: s}}})
	do := func(method, path string, rd io.Reader, ct string) ([]byte, int, error) {
		req, err := http.NewRequest(method, APIURL+path, rd)
		if err != nil {
			return nil, 0, err
		}
		req.Header.Set("User-Agent", fetch.UserAgent)
		if ct != "" {
			req.Header.Set("Content-Type", ct)
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, 0, err
		}
		defer resp.Body.Close()
		b, err := io.ReadAll(io.LimitReader(resp.Body, 256<<20))
		return b, resp.StatusCode, err
	}
	b, code, err := do("POST", "/queue", bytes.NewReader(body), "application/json")
	if err != nil || code/100 != 2 {
		return nil, fmt.Errorf("GEBCO queue request (POST %s/queue): HTTP %d %s %v. The download API is undocumented and may have changed; the global NetCDF file can be used as a local source instead", APIURL, code, snippet(b), err)
	}
	var q struct {
		BasketID string `json:"basketId"`
	}
	if json.Unmarshal(b, &q) != nil || q.BasketID == "" {
		return nil, fmt.Errorf("GEBCO queue answer has no basketId: %s", snippet(b))
	}
	for {
		b, code, err = do("GET", "/queue/status/"+q.BasketID, nil, "")
		if err != nil || code != 200 {
			return nil, fmt.Errorf("GEBCO status of %s: HTTP %d %s %v", q.BasketID, code, snippet(b), err)
		}
		var st struct {
			Status string `json:"status"`
		}
		json.Unmarshal(b, &st)
		switch st.Status {
		case "finished":
			b, code, err = do("GET", "/queue/download/"+q.BasketID, nil, "")
			if err != nil || code != 200 {
				return nil, fmt.Errorf("GEBCO download of %s: HTTP %d %v", q.BasketID, code, err)
			}
			return b, nil
		case "failed", "error":
			return nil, fmt.Errorf("GEBCO basket %s failed: %s", q.BasketID, snippet(b))
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("GEBCO basket %s not finished after %s (status %q)", q.BasketID, Timeout, st.Status)
		}
		time.Sleep(Poll)
	}
}

// extract writes the elevation and TID GeoTIFFs of the zip as gebco.tif and gebco-tid.tif. Their names carry the
// release and bounds (gebco_2026_n50.2_..._geotiff.tif and gebco_2026_tid_..._geotiff.tif).
func extract(zipBytes []byte, dir string) error {
	zr, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
	if err != nil {
		return fmt.Errorf("GEBCO answer is not a zip: %w", err)
	}
	var got [2]bool
	for _, f := range zr.File {
		name := strings.ToLower(filepath.Base(f.Name))
		if !strings.HasSuffix(name, ".tif") {
			continue
		}
		dst := "gebco.tif"
		idx := 0
		if strings.Contains(name, "_tid_") {
			dst, idx = "gebco-tid.tif", 1
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		b, err := io.ReadAll(io.LimitReader(rc, 1<<30))
		rc.Close()
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, dst), b, 0o644); err != nil {
			return err
		}
		got[idx] = true
	}
	if !got[0] || !got[1] {
		return fmt.Errorf("GEBCO zip lacks the elevation or the TID GeoTIFF (elevation %v, tid %v)", got[0], got[1])
	}
	return nil
}

func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}
