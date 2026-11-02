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
	"strings"

	"github.com/shaaanya/OceanBuilder/internal/world"
)

// unityPkg is the UPM package com.oceanbuilder.world (runtime tile index, editor importer,
// optional Addressables module), installed as an embedded package under Packages/.
//
//go:embed all:unitypkg
var unityPkg embed.FS

// UnityPackageName is the folder name under Packages/.
const UnityPackageName = "com.oceanbuilder.world"

// Unity copies a built world into a Unity project and installs the editor
// importer. Run `Ocean/Import World` in the editor, or in batchmode:
//
//	Unity -batchmode -projectPath <proj> -executeMethod OceanWorldImporter.ImportFromCommandLine -quit
//
// maxLevel is the highest LOD level whose tiles are copied (0 = level 0 only). The importer reads levels up to
// -oceanMaxLevel, so the files of every level it is asked for must be present.
func Unity(worldDir, project string, maxLevel int) (string, error) {
	m, err := world.LoadManifest(worldDir)
	if err != nil {
		return "", err
	}
	if m.CRS.Kind == "tmerc" {
		return "", fmt.Errorf("world %s is in a local transverse Mercator grid (schema 3); the Unity importer has only been checked with UTM worlds (schema 2) and refuses this one rather than guess", m.Name)
	}
	if _, err := os.Stat(filepath.Join(project, "Assets")); err != nil {
		return "", fmt.Errorf("%s does not look like a Unity project (no Assets folder)", project)
	}
	dst := filepath.Join(project, "Assets", "OceanWorld", m.Name)
	files := []string{world.ManifestFile, "ATTRIBUTION.txt"}
	if maxLevel < 0 || maxLevel >= len(m.LOD.Levels) {
		return "", fmt.Errorf("world %s has LOD levels 0..%d; cannot export up to level %d", m.Name, len(m.LOD.Levels)-1, maxLevel)
	}
	for k := 0; k <= maxLevel; k++ {
		for _, t := range m.TilesAt(k) {
			files = append(files, t.Height, t.Depth, t.Shore)
		}
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
	if err := installPackage(pkg); err != nil {
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

// installPackage writes the embedded UPM package into pkg. Files of an earlier version that the new one no longer has
// are removed; Unity's .meta files for files and folders that still exist are kept, so script GUIDs (and the scenes and
// prefabs that reference the scripts) survive a re-export.
func installPackage(pkg string) error {
	want := map[string]bool{}
	err := fs.WalkDir(unityPkg, "unitypkg", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path != "unitypkg" {
			want[filepath.FromSlash(path[len("unitypkg/"):])] = true
		}
		return nil
	})
	if err != nil {
		return err
	}
	if _, err := os.Stat(pkg); err == nil {
		err = filepath.WalkDir(pkg, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			rel, _ := filepath.Rel(pkg, path)
			if strings.HasSuffix(rel, ".meta") && want[strings.TrimSuffix(rel, ".meta")] {
				return nil
			}
			if !want[rel] {
				return os.Remove(path)
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	return fs.WalkDir(unityPkg, "unitypkg", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := unityPkg.ReadFile(path)
		if err != nil {
			return err
		}
		out := filepath.Join(pkg, filepath.FromSlash(path[len("unitypkg/"):]))
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return err
		}
		return os.WriteFile(out, b, 0o644)
	})
}
