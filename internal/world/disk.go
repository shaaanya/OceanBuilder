package world

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
)

// Disk-space preflight. A 6 x 5 degree attempt once filled the disk of the development machine, so the build checks the
// free space before it downloads or writes anything large and refuses with the numbers instead of failing half-way.
//
// The constants are measured, not derived (scripts/ladder.py, 2026-11-02, macOS arm64, docs/performance.md): the part of the
// peak disk use that is not downloads was 14..39 bytes per level-0 cell on seven regions (5..30 MiB), and the GMRT files
// (topo + mask, float32 each) came to 1.4x the node count x 8 bytes on the largest region. Both are rounded up.
const (
	workBytesPerCell = 48        // stage rasters + tiles + masks + LOD pyramid, per level-0 cell
	workFixedBytes   = 16 << 20  // coastline / seamark GeoJSON, manifest, previews
	gmrtBytesPerNode = 12        // topo + mask pieces; measured 8 x 1.4 on the 3-piece Azores request
	diskSafetyMargin = 1.25      // free space must exceed the estimate by this factor
	diskAlwaysLeave  = 256 << 20 // and leave at least this much free after the estimate
)

// FreeBytes reports the space available to the current user on the filesystem holding path (the nearest existing
// parent is used). It returns ok = false where the platform has no implementation, and the check is then skipped.
func FreeBytes(path string) (free uint64, ok bool) {
	p, err := filepath.Abs(path)
	if err != nil {
		return 0, false
	}
	for {
		if _, err := os.Stat(p); err == nil {
			break
		}
		parent := filepath.Dir(p)
		if parent == p {
			return 0, false
		}
		p = parent
	}
	return freeBytes(p)
}

// WorkDiskEstimate is the disk use of a build apart from downloads, from the number of level-0 cells.
func WorkDiskEstimate(cells int) int64 {
	return int64(cells)*workBytesPerCell + workFixedBytes
}

// GMRTDownloadEstimate is the disk use of fetching pieces of the given areas (square metres of ground) at the service's
// finest cell size refM.
func GMRTDownloadEstimate(areasM2 []float64, refM float64) int64 {
	var nodes float64
	for _, a := range areasM2 {
		nodes += a / (refM * refM)
	}
	return int64(math.Ceil(nodes * gmrtBytesPerNode))
}

// CheckDisk fails when need bytes (times the safety margin) do not fit on the filesystem holding dir. what names the step
// for the message. A filesystem whose free space cannot be read passes.
func CheckDisk(dir string, need int64, what string) error {
	free, ok := FreeBytes(dir)
	if !ok {
		return nil
	}
	want := uint64(float64(need)*diskSafetyMargin) + diskAlwaysLeave
	if free >= want {
		return nil
	}
	return fmt.Errorf("not enough disk space for %s: need about %s (estimate %s plus a %.0f%% margin and %s left free) on the filesystem of %s, which has %s free. "+
		"Free space, point -cache / -out at a bigger disk, or reduce the area or raise cell_m (ocean world check shows the estimate; docs/performance.md)",
		what, human(int64(want)), human(need), 100*(diskSafetyMargin-1), human(diskAlwaysLeave), dir, human(int64(free)))
}

func human(b int64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.1f GiB", float64(b)/(1<<30))
	default:
		return fmt.Sprintf("%.0f MiB", float64(b)/(1<<20))
	}
}
