package world

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"

	"github.com/ocean-compiler/ocean/internal/fetch"
	"github.com/ocean-compiler/ocean/internal/gdal"
)

// Composition. Every dataset is warped once onto the world grid (so tile edges and levels are cut from one
// raster, never resampled per tile), then each cell takes its value from the best-ranked dataset that has a valid
// value there. The choice is a pure function of the ranked dataset list and the warped values, so it does not depend
// on how the grid is later windowed or in which order tiles are processed. Nothing is blended or filled here: a cell
// no dataset covers stays nodata.

// CompositionStats explains the outcome of the composition.
type CompositionStats struct {
	Order    []string      `json:"order"` // dataset ids, best first
	Datasets []DatasetUse  `json:"datasets"`
	Overlaps []OverlapStat `json:"overlaps,omitempty"`
	Seams    []SeamStat    `json:"seams,omitempty"`
	Note     string        `json:"note"`
}

// DatasetUse counts what happened to one dataset's cells.
type DatasetUse struct {
	ID         string `json:"id"`
	Index      int    `json:"index"`
	Valid      int    `json:"cells_valid"`      // cells where the dataset had a valid value it was allowed to supply
	Selected   int    `json:"cells_selected"`   // cells that took their value from this dataset
	Overridden int    `json:"cells_overridden"` // valid cells that a better-ranked dataset supplied instead
}

// OverlapStat compares the chosen value with another valid value at the same cells. A large median says the two
// datasets disagree systematically (a vertical offset, a registration error), whatever their declared datums say.
type OverlapStat struct {
	Winner  string  `json:"winner"`
	Other   string  `json:"other"`
	Cells   int     `json:"cells"`
	Water   int     `json:"water_cells"`
	MedianM float64 `json:"median_diff_m"` // winner minus other, over cells that are water in both (bins of 0.05 m)
	P95AbsM float64 `json:"p95_abs_diff_m"`
	RMSM    float64 `json:"rms_diff_m"`
}

// SeamStat measures the step across the places where the chosen dataset changes between adjacent water cells.
type SeamStat struct {
	A       string  `json:"a"`
	B       string  `json:"b"`
	Pairs   int     `json:"adjacent_pairs"`
	MedianM float64 `json:"median_step_m"`
	P90M    float64 `json:"p90_step_m"`
	MaxM    float64 `json:"max_step_m"`
}

const (
	histRange = 400.0
	histStep  = 0.05
	histBins  = int(2*histRange/histStep) + 1
)

// hist is a fixed-bin histogram of differences: bounded memory, exact counts, so the same inputs give the same
// quantiles in any processing order.
type hist struct {
	c     []uint32
	n     int
	sumSq float64
	maxAb float64
}

func newHist() *hist { return &hist{c: make([]uint32, histBins)} }

func (h *hist) add(d float64) {
	i := int(math.Round((d + histRange) / histStep))
	i = min(max(i, 0), histBins-1)
	h.c[i]++
	h.n++
	h.sumSq += d * d
	h.maxAb = math.Max(h.maxAb, math.Abs(d))
}

func (h *hist) quantile(q float64) float64 {
	if h.n == 0 {
		return 0
	}
	target, acc := int(q*float64(h.n-1)), 0
	for i, c := range h.c {
		acc += int(c)
		if acc > target {
			return float64(i)*histStep - histRange
		}
	}
	return histRange
}

func (h *hist) quantileAbs(q float64) float64 {
	if h.n == 0 {
		return 0
	}
	abs := map[int]int{}
	for i, c := range h.c {
		if c > 0 {
			abs[int(math.Round(math.Abs(float64(i)*histStep-histRange)/histStep))] += int(c)
		}
	}
	keys := make([]int, 0, len(abs))
	for k := range abs {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	target, acc := int(q*float64(h.n-1)), 0
	for _, k := range keys {
		acc += abs[k]
		if acc > target {
			return float64(k) * histStep
		}
	}
	return histRange
}

func validF(v float32) bool { return v > noData+1 && !math.IsNaN(float64(v)) }

// warpDataset brings a dataset's value (and survey mask) rasters onto the world grid, using the stage cache.
func (b *bctx) warpDataset(d *dataset) error {
	method := b.cfg.Resampling
	if d.spec.Resampling != "" {
		method = d.spec.Resampling
	}
	vkey := ""
	if d.vapplied != nil && d.vapplied.Kind == "proj-grid" {
		vkey = d.vapplied.key()
	}
	warp := func(files, hashes []string, kind, method string) (string, string, error) {
		if err := gdal.Ctx.Err(); err != nil {
			return "", "", err
		}
		srcNodata := "-" // a pointer would hash as an address: print the value
		if d.spec.Nodata != nil {
			srcNodata = fmt.Sprint(*d.spec.Nodata)
		}
		key := stageKey("warp-1", hashes, kind, method, b.te, b.W, b.H, b.srs, b.gver, vkey, srcNodata, d.srcSRS)
		name := "warp:" + d.spec.ID + ":" + kind
		path := filepath.Join(b.opt.CacheDir, "stages", key[:24]+"-"+kind+".bin")
		if err := hook("start:" + name); err != nil {
			return "", "", err
		}
		if fi, err := os.Stat(path); err == nil && fi.Size() == int64(b.W)*int64(b.H)*4 {
			if sum := b.reuseSealed(path, name); sum != "" {
				b.opt.logf("stage cache hit: %s %s", d.spec.ID, kind)
				return path, sum, nil
			}
		} else if err == nil {
			b.opt.logf("WARNING: stage %s: cached file %s has %d bytes, want %d; rebuilding", name, filepath.Base(path), fi.Size(), int64(b.W)*int64(b.H)*4)
			os.Remove(path)
			os.Remove(path + ".seal")
		}
		tmp := filepath.Join(b.work, fmt.Sprintf("%s-%s.bin", d.spec.ID, kind))
		args := []string{"-overwrite", "-q", "-t_srs", b.srs, "-te", b.te[0], b.te[1], b.te[2], b.te[3],
			"-ts", fmt.Sprint(b.W), fmt.Sprint(b.H), "-r", method, "-ot", "Float32", "-dstnodata", fmt.Sprint(noData), "-of", "ENVI"}
		if d.srcSRS != "" {
			args = append(args, "-s_srs", d.srcSRS)
		}
		if d.spec.Nodata != nil && kind == "dem" {
			args = append(args, "-srcnodata", fmt.Sprint(*d.spec.Nodata))
		}
		if _, err := gdal.Run("gdalwarp", append(append(args, files...), tmp)...); err != nil {
			return "", "", err
		}
		if err := hook("mid:" + name); err != nil {
			return "", "", err
		}
		if os.MkdirAll(filepath.Dir(path), 0o755) == nil && os.Rename(tmp, path) == nil {
			sum, err := sealFile(path)
			if err != nil {
				return "", "", err
			}
			if err := hook("done:" + name); err != nil {
				return "", "", err
			}
			return path, sum, nil
		}
		sum, _, err := hashFile(tmp) // cache directory not writable: use the private copy, uncached
		return tmp, sum, err
	}
	var hashes []string
	for _, p := range d.vals {
		hashes = append(hashes, p.SHA256)
	}
	var err error
	if d.valPath, d.valSHA, err = warp(d.valSrc, hashes, "dem", method); err != nil {
		return err
	}
	if len(d.masks) > 0 {
		var mf, mh []string
		for _, p := range d.masks {
			mf, mh = append(mf, p.File), append(mh, p.SHA256)
		}
		// nearest neighbour keeps the survey mask binary
		if d.maskPath, d.maskSHA, err = warp(mf, mh, "mask", "near"); err != nil {
			return err
		}
	}
	return nil
}

// composeLevel0 writes the level-0 layer files: for every cell, the value of the first admissible dataset in
// ds (best first), the source kind, and the dataset index. Rows are streamed, so memory is O(rows x width).
func (b *bctx) composeLevel0(ds []*dataset, out *layerFiles) (*CompositionStats, error) {
	W, H := b.W, b.H
	vals := make([]*rawF32, len(ds))
	masks := make([]*rawF32, len(ds))
	for i, d := range ds {
		f, err := openF32(d.valPath, W, H)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		vals[i] = f
		if d.maskPath != "" {
			mf, err := openF32(d.maskPath, W, H)
			if err != nil {
				return nil, err
			}
			defer mf.Close()
			masks[i] = mf
		}
	}
	cs := &CompositionStats{Note: "Each cell takes its value from the best-ranked dataset with a valid value there. Overlap and seam figures compare datasets where they meet; they are evidence about agreement, not corrections."}
	use := make([]DatasetUse, len(ds))
	for i, d := range ds {
		use[i] = DatasetUse{ID: d.spec.ID, Index: d.idx + 1}
		cs.Order = append(cs.Order, d.spec.ID)
	}
	overlap := map[[2]int]*hist{}
	overlapN := map[[2]int]*[2]int{} // cells, water cells
	seam := map[[2]int]*hist{}
	w, err := newLayerWriter(out)
	if err != nil {
		return nil, err
	}
	rows := max(1, min(256, int(b.memBytes()/4/int64(W)/int64(8*len(ds)+16))))
	selRow, prevSel := make([]int, W), make([]int, W)
	for i := range prevSel {
		prevSel[i] = -1
	}
	prevDem := make([]float32, W)
	dem, kind, prov := make([]float32, W), make([]uint8, W), make([]uint8, W)
	for r0 := 0; r0 < H; r0 += rows {
		if err := gdal.Ctx.Err(); err != nil {
			w.close()
			return nil, err
		}
		nr := min(rows, H-r0)
		vb := make([][]float32, len(ds))
		mb := make([][]float32, len(ds))
		for i := range ds {
			if vb[i], err = vals[i].rect(r0, 0, nr, W); err != nil {
				w.close()
				return nil, err
			}
			if masks[i] != nil {
				if mb[i], err = masks[i].rect(r0, 0, nr, W); err != nil {
					w.close()
					return nil, err
				}
			}
		}
		for rr := 0; rr < nr; rr++ {
			for x := 0; x < W; x++ {
				sel := -1
				var selV float32
				for i, d := range ds {
					v := vb[i][rr*W+x]
					if !validF(v) {
						continue
					}
					if d.spec.Part == "survey" || d.spec.Part == "background" {
						surv := mb[i] != nil && validF(mb[i][rr*W+x])
						if surv != (d.spec.Part == "survey") {
							continue
						}
					}
					v += float32(d.offsetM)
					if d.spec.ValidMax != nil && v > float32(*d.spec.ValidMax) {
						continue
					}
					use[i].Valid++
					if sel < 0 {
						sel, selV = i, v
						use[i].Selected++
						continue
					}
					use[i].Overridden++
					key := [2]int{sel, i}
					h := overlap[key]
					if h == nil {
						h, overlapN[key] = newHist(), &[2]int{}
						overlap[key] = h
					}
					overlapN[key][0]++
					if selV < 0 && v < 0 {
						overlapN[key][1]++
						h.add(float64(selV - v))
					}
				}
				selRow[x] = sel
				if sel < 0 {
					dem[x], kind[x], prov[x] = noData, SrcNoData, 0
					continue
				}
				d := ds[sel]
				dem[x], prov[x] = selV, uint8(d.idx+1)
				switch {
				case selV >= 0:
					kind[x] = SrcLand
				case masks[sel] != nil && validF(mb[sel][rr*W+x]):
					kind[x] = SrcSurvey
				default:
					kind[x] = qualityKind(d.spec.Quality)
				}
			}
			// seams: adjacent water cells whose chosen dataset differs (left neighbour, and the row above)
			for x := 0; x < W; x++ {
				a := selRow[x]
				if a < 0 || dem[x] >= 0 {
					continue
				}
				if x > 0 && selRow[x-1] >= 0 && selRow[x-1] != a && dem[x-1] < 0 && validF(dem[x-1]) {
					addSeam(seam, selRow[x-1], a, float64(dem[x]-dem[x-1]))
				}
				if prevSel[x] >= 0 && prevSel[x] != a && prevDem[x] < 0 && validF(prevDem[x]) {
					addSeam(seam, prevSel[x], a, float64(dem[x]-prevDem[x]))
				}
			}
			if err := w.writeRow(dem, kind, prov); err != nil {
				w.close()
				return nil, err
			}
			copy(prevSel, selRow)
			copy(prevDem, dem)
		}
	}
	if err := w.close(); err != nil {
		return nil, err
	}
	cs.Datasets = use
	for _, k := range sortedKeys(overlap) {
		h := overlap[k]
		o := OverlapStat{Winner: ds[k[0]].spec.ID, Other: ds[k[1]].spec.ID, Cells: overlapN[k][0], Water: overlapN[k][1]}
		if h.n > 0 {
			o.MedianM, o.P95AbsM, o.RMSM = h.quantile(0.5), h.quantileAbs(0.95), math.Sqrt(h.sumSq/float64(h.n))
		}
		cs.Overlaps = append(cs.Overlaps, o)
	}
	for _, k := range sortedKeys(seam) {
		h := seam[k]
		cs.Seams = append(cs.Seams, SeamStat{A: ds[k[0]].spec.ID, B: ds[k[1]].spec.ID, Pairs: h.n,
			MedianM: h.quantileAbs(0.5), P90M: h.quantileAbs(0.9), MaxM: h.maxAb})
	}
	return cs, nil
}

func sortedKeys(m map[[2]int]*hist) [][2]int {
	var keys [][2]int
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i][0] != keys[j][0] {
			return keys[i][0] < keys[j][0]
		}
		return keys[i][1] < keys[j][1]
	})
	return keys
}

// addSeam records a step between a cell of dataset a and a neighbour of dataset b (unordered pair).
func addSeam(seam map[[2]int]*hist, a, b int, step float64) {
	if a > b {
		a, b = b, a
	}
	h := seam[[2]int{a, b}]
	if h == nil {
		h = newHist()
		seam[[2]int{a, b}] = h
	}
	h.add(math.Abs(step))
}

// qualityKind is the source code of a water cell by the quality class of the dataset that supplied it.
func qualityKind(q string) uint8 {
	switch q {
	case "survey":
		return SrcSurvey
	case "regional":
		return SrcRegional
	}
	return SrcBackground
}

// uniqueProvenance returns each provenance once, in first-seen order.
func uniqueProvenance(lists ...[]*fetch.Provenance) []fetch.Provenance {
	seen := map[*fetch.Provenance]bool{}
	var out []fetch.Provenance
	for _, l := range lists {
		for _, p := range l {
			if !seen[p] {
				seen[p] = true
				out = append(out, *p)
			}
		}
	}
	return out
}
