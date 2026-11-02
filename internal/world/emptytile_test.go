package world

import (
	"os"
	"path/filepath"
	"testing"
)

// A level-0 tile without any data is written as explicit nodata (the world keeps its full grid and the validator names the
// gap); a tile above level 0 without data is omitted. Before 0.5.0 level 0 dropped it too and the validator reported a
// confusing tile-count / source-stats mismatch instead of "empty-tile".
func TestEmptyTileKeptAtLevel0DroppedAbove(t *testing.T) {
	const n = 4
	S := n + 1
	win := &raster{W: S, H: S, dem: make([]float32, S*S), src: make([]uint8, S*S), prov: make([]uint8, S*S), class: make([]uint8, S*S), shore: make([]uint8, S*S)}
	for i := range win.dem {
		win.dem[i] = noData
		win.class[i] = ClassNoData
		win.src[i] = SrcNoData
	}
	for _, level := range []int{0, 1} {
		dir := t.TempDir()
		b := &bctx{cfg: &Config{TileCells: n}, opt: Options{OutDir: dir}}
		if err := os.MkdirAll(filepath.Join(dir, "tiles"), 0o755); err != nil {
			t.Fatal(err)
		}
		slot := &tileSlot{plan: levelPlan{Level: level, SpanM: 100, OriginE: 0, OriginN: 100}}
		var tile Tile
		ok, err := b.writeTile(&tile, slot, win, 0, 0, "tiles")
		if err != nil {
			t.Fatal(err)
		}
		if level == 0 {
			if !ok || tile.NoData != 1 || tile.MinM != 0 || tile.MaxM != 0 {
				t.Fatalf("level 0 empty tile: written=%v nodata=%v range %v..%v", ok, tile.NoData, tile.MinM, tile.MaxM)
			}
			if _, err := os.Stat(filepath.Join(dir, tile.Height)); err != nil {
				t.Fatalf("level 0 empty tile has no height file: %v", err)
			}
		} else if ok {
			t.Fatal("an empty tile above level 0 must be omitted")
		}
	}
}
