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
const UserAgent = "ocean-compiler/0.2 (+https://github.com/ocean-compiler/ocean)"

// Provenance describes one downloaded source file.
type Provenance struct {
	Source      string `json:"source"`
	URL         string `json:"url"`
	RetrievedAt string `json:"retrieved_at"`
	SHA256      string `json:"sha256"`
	Bytes       int64  `json:"bytes"`
	License     string `json:"license"`
	Attribution string `json:"attribution"`
	File        string `json:"file,omitempty"` // local cache path; stripped from shipped manifests
}

// Get downloads a URL (GET, or POST form when form != nil) into
// cacheDir/<source>/<hash of request>/<name>. A cached copy is reused.
func Get(cacheDir, source, rawURL string, form url.Values, name, license, attribution string) (*Provenance, error) {
	return GetTimeout(3*time.Minute, cacheDir, source, rawURL, form, name, license, attribution)
}

// GetTimeout is Get with an explicit whole-request timeout.
func GetTimeout(timeout time.Duration, cacheDir, source, rawURL string, form url.Values, name, license, attribution string) (*Provenance, error) {
	key := rawURL + "?" + form.Encode()
	ksum := sha256.Sum256([]byte(key))
	dir := filepath.Join(cacheDir, source, hex.EncodeToString(ksum[:8]))
	file := filepath.Join(dir, name)
	meta := file + ".json"
	if b, err := os.ReadFile(meta); err == nil {
		var p Provenance
		if json.Unmarshal(b, &p) == nil {
			// A cached file that no longer matches its recorded hash is corrupt or was replaced: refetch, never reuse.
			if sum, err := SHA256File(file); err == nil && sum == p.SHA256 {
				p.License, p.Attribution = license, attribution // terms may have been corrected since the download
				p.File = file                                   // the stored path was relative to the cwd of the original run
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
	resp, err := (&http.Client{Timeout: timeout}).Do(req)
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

// Derived caches files that a function produces from a remote service (a window read from a COG, an unzipped subset,
// a mask computed from a layer). The result is stored like a download: cacheDir/<source>/<hash of key>/<name> with a
// sidecar holding url (a description of what was requested), retrieval time and the sha256 of the produced file, so a
// later run reuses it only while the bytes still match. produce writes every file in names into dir.
func Derived(cacheDir, source, key, url, license, attribution string, names []string, produce func(dir string) error) ([]*Provenance, error) {
	ksum := sha256.Sum256([]byte(key))
	dir := filepath.Join(cacheDir, source, hex.EncodeToString(ksum[:8]))
	read := func() ([]*Provenance, bool) {
		var out []*Provenance
		for _, n := range names {
			b, err := os.ReadFile(filepath.Join(dir, n+".json"))
			if err != nil {
				return nil, false
			}
			var p Provenance
			if json.Unmarshal(b, &p) != nil {
				return nil, false
			}
			file := filepath.Join(dir, n)
			if sum, err := SHA256File(file); err != nil || sum != p.SHA256 {
				return nil, false
			}
			p.License, p.Attribution, p.File = license, attribution, file
			out = append(out, &p)
		}
		return out, true
	}
	if out, ok := read(); ok {
		return out, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	if err := produce(dir); err != nil {
		return nil, fmt.Errorf("%s: %w", source, err)
	}
	var out []*Provenance
	now := time.Now().UTC().Format(time.RFC3339)
	for _, n := range names {
		file := filepath.Join(dir, n)
		fi, err := os.Stat(file)
		if err != nil {
			return nil, fmt.Errorf("%s: expected output %s: %w", source, n, err)
		}
		sum, err := SHA256File(file)
		if err != nil {
			return nil, err
		}
		p := &Provenance{Source: source, URL: url, RetrievedAt: now, SHA256: sum, Bytes: fi.Size(), License: license, Attribution: attribution, File: file}
		b, _ := json.MarshalIndent(p, "", "  ")
		if err := os.WriteFile(file+".json", b, 0o644); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}
