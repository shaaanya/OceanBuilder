// Package export writes engine-specific deliverables from a built world.
// Adapters stay thin: all processing happened in the Go core, the engine
// side only reads world.manifest.json and the tile files.
package export

import (
	_ "embed"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/ocean-compiler/ocean/internal/world"
)

//go:embed unity/OceanWorldImporter.cs
var unityImporter []byte

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
	for _, t := range m.Tiles {
		files = append(files, t.Height, t.Depth, t.Shore)
	}
	for _, f := range files {
		if err := copyFile(filepath.Join(worldDir, f), filepath.Join(dst, f)); err != nil {
			return "", err
		}
	}
	ed := filepath.Join(project, "Assets", "Editor")
	if err := os.MkdirAll(ed, 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(ed, "OceanWorldImporter.cs"), unityImporter, 0o644); err != nil {
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
