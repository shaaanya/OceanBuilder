// Package fetch downloads source data into a content-addressed cache and
// records provenance for each download.
package fetch

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// UserAgent identifies the tool to data providers (Overpass rejects empty UAs).
const UserAgent = "ocean-compiler/0.1 (+https://github.com/ocean-compiler/ocean)"

// Provenance describes one downloaded source file.
type Provenance struct {
	Source      string `json:"source"`
	URL         string `json:"url"`
	RetrievedAt string `json:"retrieved_at"`
	SHA256      string `json:"sha256"`
	Bytes       int64  `json:"bytes"`
	License     string `json:"license"`
	Attribution string `json:"attribution"`
	File        string `json:"file"`
}

// Get downloads a URL (GET, or POST form when form != nil) into
// cacheDir/<source>/<hash of request>/<name>. A cached copy is reused.
func Get(cacheDir, source, rawURL string, form url.Values, name, license, attribution string) (*Provenance, error) {
	key := rawURL + "?" + form.Encode()
	ksum := sha256.Sum256([]byte(key))
	dir := filepath.Join(cacheDir, source, hex.EncodeToString(ksum[:8]))
	file := filepath.Join(dir, name)
	meta := file + ".json"
	if b, err := os.ReadFile(meta); err == nil {
		var p Provenance
		if json.Unmarshal(b, &p) == nil {
			if _, err := os.Stat(file); err == nil {
				return &p, nil
			}
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	var req *http.Request
	var err error
	if form != nil {
		req, err = http.NewRequest("POST", rawURL, strings.NewReader(form.Encode()))
		if req != nil {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
	} else {
		req, err = http.NewRequest("GET", rawURL, nil)
	}
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Accept", "*/*")
	resp, err := (&http.Client{Timeout: 3 * time.Minute}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", source, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		return nil, fmt.Errorf("%s: HTTP %d: %s", source, resp.StatusCode, strings.TrimSpace(string(b)))
	}
	tmp := file + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return nil, err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), resp.Body)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp)
		return nil, fmt.Errorf("%s: download: %w", source, err)
	}
	if err := os.Rename(tmp, file); err != nil {
		return nil, err
	}
	p := &Provenance{Source: source, URL: rawURL, RetrievedAt: time.Now().UTC().Format(time.RFC3339),
		SHA256: hex.EncodeToString(h.Sum(nil)), Bytes: n, License: license, Attribution: attribution, File: file}
	if form != nil {
		p.URL = rawURL + " (POST " + form.Encode() + ")"
	}
	b, _ := json.MarshalIndent(p, "", "  ")
	return p, os.WriteFile(meta, b, 0o644)
}

// SHA256File returns the hex sha256 of a file.
func SHA256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
