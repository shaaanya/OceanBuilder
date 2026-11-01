// Package export writes engine-specific deliverables from a built world.
// Adapters stay thin: all processing happened in the Go core, the engine
// side only reads world.manifest.json and the tile files.
package export

import (
	"embed"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/ocean-compiler/ocean/internal/world"
)

// unityPkg is the UPM package com.oceancompiler.world (runtime tile index, editor importer,
// optional Addressables module), installed as an embedded package under Packages/.
//
//go:embed all:unitypkg
var unityPkg embed.FS

// UnityPackageName is the folder name under Packages/.
const UnityPackageName = "com.oceancompiler.world"

// Unity copies a built world into a Unity project and installs the editor
// importer. Run `Ocean/Import World` in the editor, or in batchmode:
//
//	Unity -batchmode -projectPath <proj> -executeMethod OceanWorldImporter.ImportFromCommandLine -quit
func Unity(worldDir, project string) (string, error) {
	m, err := world.LoadManifest(worldDir)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(filepath.Join(project, "Assets")); err != nil {
		return "", fmt.Errorf("%s does not look like a Unity project (no Assets folder)", project)
	}
	dst := filepath.Join(project, "Assets", "OceanWorld", m.Name)
	files := []string{world.ManifestFile, "ATTRIBUTION.txt"}
	for _, t := range m.TilesAt(0) { // the Unity importer reads level 0 only
		files = append(files, t.Height, t.Depth, t.Shore)
	}
	// A rebuild without seamarks must not leave an older seamarks.geojson for the importer to pick up.
	if sm := m.Files["seamarks"]; sm != "" {
		files = append(files, sm)
	} else if err := os.Remove(filepath.Join(dst, "seamarks.geojson")); err != nil && !os.IsNotExist(err) {
		return "", err
	}
	for _, f := range files {
		if err := copyFile(filepath.Join(worldDir, f), filepath.Join(dst, f)); err != nil {
			return "", err
		}
	}
	// The importer used to be a single script in Assets/Editor; it would clash with the package's class.
	if err := os.Remove(filepath.Join(project, "Assets", "Editor", "OceanWorldImporter.cs")); err != nil && !os.IsNotExist(err) {
		return "", err
	}
	pkg := filepath.Join(project, "Packages", UnityPackageName)
	if err := os.RemoveAll(pkg); err != nil {
		return "", err
	}
	err = fs.WalkDir(unityPkg, "unitypkg", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := unityPkg.ReadFile(path)
		if err != nil {
			return err
		}
		out := filepath.Join(pkg, path[len("unitypkg/"):])
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return err
		}
		return os.WriteFile(out, b, 0o644)
	})
	if err != nil {
		return "", err
	}
	return dst, nil
}

func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
