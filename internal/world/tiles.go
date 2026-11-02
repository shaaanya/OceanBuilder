package world

import (
	"encoding/binary"
	"fmt"
	"image"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	"github.com/shaaanya/OceanBuilder/internal/gdal"
)

// Windowed tile writing. A level lives in layer files; tiles are cut from rectangles ("blocks") of tiles read with
// a one-sample halo, which is all the derived layers need (shore looks at 4-neighbours). The tile bytes depend only
// on the samples, never on the block size, which tests assert by building with several window sizes.

// blockTiles is the block edge in tiles: from the memory budget unless a test forces it.
func (b *bctx) blockTiles() int {
	if b.opt.WindowTiles > 0 {
		return b.opt.WindowTiles
	}
	N := float64(b.cfg.TileCells)
	cells := float64(b.memBytes()) / 40 // the window rasters (dem, kind, dataset, class, shore) plus encoded copies
	return max(1, int((math.Sqrt(cells)-3)/N))
}

// writeLevel cuts a level into tiles, block by block, and returns the tiles (tiles with no valid sample are dropped)
// and the number of shore samples (each world sample counted once, shared edges excluded).
func (b *bctx) writeLevel(lay *layerFiles, p levelPlan, slots []tileSlot) ([]Tile, int, error) {
	N := b.cfg.TileCells
	dir := "tiles/" + fmt.Sprint(p.Level)
	if err := os.MkdirAll(filepath.Join(b.opt.OutDir, dir), 0o755); err != nil {
		return nil, 0, err
	}
	if err := lay.open(); err != nil {
		return nil, 0, err
	}
	defer lay.close()
	bn := b.blockTiles()
	tiles := make([]Tile, len(slots))
	keep := make([]bool, len(slots))
	errs := make([]error, len(slots))
	shores := 0
	for br := 0; br < p.TY; br += bn {
		for bc := 0; bc < p.TX; bc += bn {
			if err := gdal.Ctx.Err(); err != nil {
				return nil, 0, err
			}
			nr, nc := min(bn, p.TY-br), min(bn, p.TX-bc)
			r0, c0 := br*N-1, bc*N-1
			h, w := nr*N+3, nc*N+3 // core nr*N+1 samples plus a halo of one on each side
			win := &raster{W: w, H: h}
			var err error
			if win.dem, err = lay.dem.rect(r0, c0, h, w); err != nil {
				return nil, 0, err
			}
			if win.src, err = lay.kind.rect(r0, c0, h, w, 0); err != nil {
				return nil, 0, err
			}
			if win.prov, err = lay.prov.rect(r0, c0, h, w, 0); err != nil {
				return nil, 0, err
			}
			win.derive(b.cfg.DepthBands)
			lastR, lastC := br+nr == p.TY, bc+nc == p.TX
			for i := 1; i <= nr*N+1; i++ {
				if i == nr*N+1 && !lastR {
					break
				}
				for j := 1; j <= nc*N+1; j++ {
					if j == nc*N+1 && !lastC {
						break
					}
					if win.shore[i*w+j] != 0 {
						shores++
					}
				}
			}
			var wg sync.WaitGroup
			sem := make(chan struct{}, runtime.NumCPU())
			for r := br; r < br+nr; r++ {
				for c := bc; c < bc+nc; c++ {
					i := r*p.TX + c
					wg.Add(1)
					sem <- struct{}{}
					go func(i int, offR, offC int) {
						defer wg.Done()
						defer func() { <-sem }()
						keep[i], errs[i] = b.writeTile(&tiles[i], &slots[i], win, offR, offC, dir)
					}(i, (r-br)*N+1, (c-bc)*N+1)
				}
			}
			wg.Wait()
			for r := br; r < br+nr; r++ {
				for c := bc; c < bc+nc; c++ {
					if errs[r*p.TX+c] != nil {
						return nil, 0, errs[r*p.TX+c]
					}
				}
			}
		}
	}
	var res []Tile
	for i := range slots {
		if keep[i] {
			res = append(res, tiles[i])
		}
	}
	return res, shores, nil
}

// writeTile writes one tile's files and fills its index entry. offR/offC locate the tile's first sample inside the
// window. It reports false (and writes nothing) when a tile above level 0 holds no valid sample (padded or empty upper-level
// tiles); level 0 tiles are always written, an empty one as all nodata.
func (b *bctx) writeTile(t *Tile, s *tileSlot, r *raster, offR, offC int, dir string) (bool, error) {
	N := b.cfg.TileCells
	S := N + 1
	p := s.plan
	id := tileID(p.Level, s.x, s.y)
	hbuf := make([]byte, S*S*4)
	cls := image.NewGray(image.Rect(0, 0, S, S))
	shr := image.NewGray(image.Rect(0, 0, S, S))
	src := image.NewGray(image.Rect(0, 0, S, S))
	dsl := image.NewGray(image.Rect(0, 0, S, S))
	mn, mx := math.Inf(1), math.Inf(-1)
	var nd, water, measured int
	for i := 0; i < S; i++ {
		for j := 0; j < S; j++ {
			gi := (offR+i)*r.W + offC + j
			v := r.dem[gi]
			if v <= noData+1 {
				v = noData
				nd++
			} else {
				mn, mx = math.Min(mn, float64(v)), math.Max(mx, float64(v))
				if v < 0 {
					water++
					if r.src[gi] == SrcSurvey {
						measured++
					}
				}
			}
			binary.LittleEndian.PutUint32(hbuf[(i*S+j)*4:], math.Float32bits(v))
			cls.Pix[i*S+j], shr.Pix[i*S+j], src.Pix[i*S+j], dsl.Pix[i*S+j] = r.class[gi], r.shore[gi], r.src[gi], r.prov[gi]
		}
	}
	if nd == S*S {
		if p.Level > 0 {
			return false, nil // a coarse tile with no valid sample (past the world's edge, or inside a gap) is omitted
		}
		// Level 0 always has the whole grid: a tile without data is written as all nodata so the gap is explicit, and the
		// validator reports it ("empty-tile"). Its elevation range is undefined; 0..0 is recorded.
		mn, mx = 0, 0
	}
	*t = Tile{ID: id, Level: p.Level, X: s.x, Y: s.y, Col: s.col, Row: s.row,
		Height: dir + "/" + id + ".height.f32", Depth: dir + "/" + id + ".depth.png",
		Shore: dir + "/" + id + ".shore.png", Source: dir + "/" + id + ".source.png", Dataset: dir + "/" + id + ".dataset.png",
		MinM: mn, MaxM: mx, NoData: float64(nd) / float64(S*S), Water: float64(water) / float64(S*S)}
	t.BoundsUTM = [4]float64{p.OriginE + float64(s.col)*p.SpanM, p.OriginN - float64(s.row+1)*p.SpanM,
		p.OriginE + float64(s.col+1)*p.SpanM, p.OriginN - float64(s.row)*p.SpanM}
	if len(s.corners) == 4 {
		var lo, la []float64
		for _, c := range s.corners {
			lo, la = append(lo, c[0]), append(la, c[1])
		}
		t.BoundsWGS = [4]float64{minF(lo), minF(la), maxF(lo), maxF(la)}
	}
	if water > 0 {
		t.Measured = float64(measured) / float64(water)
	}
	t.SHA256.Height = hashHex(hbuf)
	if err := os.WriteFile(filepath.Join(b.opt.OutDir, t.Height), hbuf, 0o644); err != nil {
		return false, err
	}
	for _, f := range []struct {
		path string
		img  *image.Gray
		hash *string
	}{{t.Depth, cls, &t.SHA256.Depth}, {t.Shore, shr, &t.SHA256.Shore}, {t.Source, src, &t.SHA256.Source}, {t.Dataset, dsl, &t.SHA256.Dataset}} {
		h, err := writePNG(filepath.Join(b.opt.OutDir, f.path), f.img)
		if err != nil {
			return false, err
		}
		*f.hash = h
	}
	return true, nil
}

// decimateLayer builds level k's layer from level k-1 by taking every second vertex. A level vertex sits exactly on a
// level k-1 vertex, so values are real samples (never averaged or interpolated) and chained decimation equals decimating
// level 0 by 2^k. Vertices outside the previous level are nodata.
func (b *bctx) decimateLayer(prev *layerFiles, pp, p levelPlan, out *layerFiles) error {
	N := b.cfg.TileCells
	if err := prev.open(); err != nil {
		return err
	}
	defer prev.close()
	w, err := newLayerWriter(out)
	if err != nil {
		return err
	}
	// Level vertex (row, col) is previous-level column colBase+2*col and row rowBase+2*row (north-based).
	colBase := N * (2*p.X0 - pp.X0)
	rowBase := N * ((pp.Y1 + 1) - 2*(p.Y1+1))
	dem, kind, prov := make([]float32, p.W), make([]uint8, p.W), make([]uint8, p.W)
	for row := 0; row < p.H; row++ {
		if err := gdal.Ctx.Err(); err != nil {
			w.close()
			return err
		}
		pr := rowBase + 2*row
		var pd []float32
		var pk, pv []uint8
		if pr >= 0 && pr < prev.H {
			// rows pr-1..pr+1 (out-of-range cells read as nodata) so smoothing sees the full 3x3 neighbourhood
			if pd, err = prev.dem.rect(pr-1, 0, 3, prev.W); err != nil {
				w.close()
				return err
			}
			if pk, err = prev.kind.rect(pr-1, 0, 3, prev.W, 0); err != nil {
				w.close()
				return err
			}
			if pv, err = prev.prov.rect(pr, 0, 1, prev.W, 0); err != nil {
				w.close()
				return err
			}
		}
		for c := 0; c < p.W; c++ {
			pc := colBase + 2*c
			if pd == nil || pc < 0 || pc >= prev.W {
				dem[c], kind[c], prov[c] = noData, SrcNoData, 0
				continue
			}
			dem[c], kind[c] = lodSample(b.cfg.LODMethod, pd, pk, prev.W, pc)
			prov[c] = pv[pc]
		}
		if err := w.writeRow(dem, kind, prov); err != nil {
			w.close()
			return err
		}
	}
	return w.close()
}

// lodSample returns the coarse sample for the previous-level vertex at column pc of the middle row of the 3-row window
// d/k (rows pr-1, pr, pr+1; each w wide). Method "shoal": a water sample becomes the shallowest water height of its 3x3
// neighbourhood (never deeper than any finer water sample within one step); land and nodata samples are unchanged, so
// the waterline never moves. Any other method keeps the sample itself (vertex decimation).
func lodSample(method string, d []float32, k []uint8, w, pc int) (float32, uint8) {
	ctr := d[w+pc]
	if method != "shoal" || !validF(ctr) || ctr >= 0 {
		return ctr, k[w+pc]
	}
	best := ctr
	for dr := 0; dr < 3; dr++ {
		for dc := pc - 1; dc <= pc+1; dc++ {
			if dc < 0 || dc >= w {
				continue
			}
			if v := d[dr*w+dc]; validF(v) && v < 0 && v > best {
				best = v
			}
		}
	}
	if best == ctr {
		return ctr, k[w+pc]
	}
	return best, SrcLODDerived
}
