package world

import (
	"strings"
	"testing"
)

func TestDiskCheckRefusesWhatCannotFit(t *testing.T) {
	dir := t.TempDir()
	free, ok := FreeBytes(dir + "/not/yet/created")
	if !ok || free == 0 {
		t.Skip("free space not readable on this platform")
	}
	if err := CheckDisk(dir, 1<<20, "a tiny step"); err != nil {
		t.Fatalf("1 MiB should fit: %v", err)
	}
	err := CheckDisk(dir, 1<<60, "an impossible step")
	if err == nil || !strings.Contains(err.Error(), "an impossible step") || !strings.Contains(err.Error(), "free") {
		t.Fatalf("want a refusal naming the step and the free space, got %v", err)
	}
}

func TestDiskEstimatesScale(t *testing.T) {
	// Azores ladder run: 1.4 M cells needed 27 MiB of work disk (measured) and 383 MiB of GMRT files; the estimates are above both.
	if got := WorkDiskEstimate(1_378_000); got < 27<<20 {
		t.Errorf("work estimate %d below the measured 27 MiB", got)
	}
	areas := []float64{351.8e3 * 226e3} // padded request, m^2
	if got := GMRTDownloadEstimate(areas, 48.46); got < 383<<20 {
		t.Errorf("download estimate %d MiB below the measured 383 MiB", got>>20)
	}
}
