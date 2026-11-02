package oceanworld_test

import (
	"testing"

	"github.com/shaaanya/OceanBuilder/pkg/oceanworld"
)

// Query-cost benchmarks on the Falmouth fixture (go test ./pkg/oceanworld -bench . -run x).

func benchWorld(b *testing.B) (*oceanworld.World, float64, float64) {
	dir, m := buildFixture(b)
	w, err := oceanworld.Open(dir)
	if err != nil {
		b.Fatal(err)
	}
	g := m.Grid
	return w, g.OriginE + g.TileSpanM*float64(g.TilesX)/2, g.OriginN - g.TileSpanM*float64(g.TilesY)/2
}

// SampleUTM with the tile already decoded: the steady-state cost of a gameplay depth query.
func BenchmarkSampleWarm(b *testing.B) {
	w, e, n := benchWorld(b)
	w.SampleUTM(e, n, 0)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w.SampleUTM(e+float64(i%200), n+float64(i%150), 0)
	}
}

// Load of a tile that is not cached: file read + 3 PNG decodes + float conversion.
func BenchmarkLoadCold(b *testing.B) {
	w, e, n := benchWorld(b)
	t := w.TileAt(e, n, 0)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w2, err := oceanworld.Open(w.Dir)
		if err != nil {
			b.Fatal(err)
		}
		if _, err := w2.Load(t); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkProfile100(b *testing.B) {
	w, e, n := benchWorld(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w.Profile(e-500, n-500, e+500, n+500, 100, 0)
	}
}
