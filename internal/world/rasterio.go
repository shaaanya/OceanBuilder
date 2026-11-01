package world

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
)

// Disk-backed rasters. A world grid can be larger than memory, so the pipeline keeps whole-grid layers in raw
// files (row-major, row 0 = north, headerless: the same bytes gdalwarp writes as ENVI) and reads rectangles.
// Cells outside the grid read as nodata / fill, which is what the in-memory code treats as "no neighbour".

// rawF32 is a W x H little-endian float32 raster on disk.
type rawF32 struct {
	f    *os.File
	W, H int
}

func openF32(path string, w, h int) (*rawF32, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	fi, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	if fi.Size() != int64(w)*int64(h)*4 {
		f.Close()
		return nil, fmt.Errorf("%s: %d bytes, want %d (%dx%d float32)", path, fi.Size(), int64(w)*int64(h)*4, w, h)
	}
	return &rawF32{f: f, W: w, H: h}, nil
}

func (r *rawF32) Close() error { return r.f.Close() }

// rect reads rows r0..r0+h-1, columns c0..c0+w-1 into a new slice; cells outside the raster are noData.
func (r *rawF32) rect(r0, c0, h, w int) ([]float32, error) {
	out := make([]float32, w*h)
	for i := range out {
		out[i] = noData
	}
	cs, ce := max(c0, 0), min(c0+w, r.W)
	if cs >= ce {
		return out, nil
	}
	buf := make([]byte, (ce-cs)*4)
	for i := 0; i < h; i++ {
		row := r0 + i
		if row < 0 || row >= r.H {
			continue
		}
		if _, err := r.f.ReadAt(buf, (int64(row)*int64(r.W)+int64(cs))*4); err != nil {
			return nil, fmt.Errorf("%s: %w", r.f.Name(), err)
		}
		dst := out[i*w+(cs-c0):]
		for k := 0; k < ce-cs; k++ {
			dst[k] = math.Float32frombits(binary.LittleEndian.Uint32(buf[k*4:]))
		}
	}
	return out, nil
}

// rawU8 is a W x H byte raster on disk.
type rawU8 struct {
	f    *os.File
	W, H int
}

func openU8(path string, w, h int) (*rawU8, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	fi, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	if fi.Size() != int64(w)*int64(h) {
		f.Close()
		return nil, fmt.Errorf("%s: %d bytes, want %d", path, fi.Size(), int64(w)*int64(h))
	}
	return &rawU8{f: f, W: w, H: h}, nil
}

func (r *rawU8) Close() error { return r.f.Close() }

// rect reads a rectangle; cells outside the raster are fill.
func (r *rawU8) rect(r0, c0, h, w int, fill uint8) ([]uint8, error) {
	out := make([]uint8, w*h)
	if fill != 0 {
		for i := range out {
			out[i] = fill
		}
	}
	cs, ce := max(c0, 0), min(c0+w, r.W)
	if cs >= ce {
		return out, nil
	}
	for i := 0; i < h; i++ {
		row := r0 + i
		if row < 0 || row >= r.H {
			continue
		}
		if _, err := r.f.ReadAt(out[i*w+(cs-c0):i*w+(ce-c0)], int64(row)*int64(r.W)+int64(cs)); err != nil {
			return nil, fmt.Errorf("%s: %w", r.f.Name(), err)
		}
	}
	return out, nil
}

// layerFiles is one level on disk: heights, per-cell kind (source code) and dataset index.
type layerFiles struct {
	W, H                        int
	demPath, kindPath, provPath string
	dem                         *rawF32
	kind, prov                  *rawU8
}

func (l *layerFiles) open() (err error) {
	if l.dem, err = openF32(l.demPath, l.W, l.H); err != nil {
		return err
	}
	if l.kind, err = openU8(l.kindPath, l.W, l.H); err != nil {
		return err
	}
	l.prov, err = openU8(l.provPath, l.W, l.H)
	return err
}

func (l *layerFiles) close() {
	if l.dem != nil {
		l.dem.Close()
	}
	if l.kind != nil {
		l.kind.Close()
	}
	if l.prov != nil {
		l.prov.Close()
	}
	l.dem, l.kind, l.prov = nil, nil, nil
}

// layerWriter streams a layer to disk one row at a time, north to south.
type layerWriter struct {
	dem, kind, prov *os.File
	rowBuf          []byte
}

func newLayerWriter(l *layerFiles) (*layerWriter, error) {
	w := &layerWriter{rowBuf: make([]byte, l.W*4)}
	var err error
	if w.dem, err = os.Create(l.demPath); err != nil {
		return nil, err
	}
	if w.kind, err = os.Create(l.kindPath); err != nil {
		return nil, err
	}
	if w.prov, err = os.Create(l.provPath); err != nil {
		return nil, err
	}
	return w, nil
}

// writeRow appends one row to all three layers.
func (w *layerWriter) writeRow(dem []float32, kind, prov []uint8) error {
	for i, v := range dem {
		binary.LittleEndian.PutUint32(w.rowBuf[i*4:], math.Float32bits(v))
	}
	if _, err := w.dem.Write(w.rowBuf[:len(dem)*4]); err != nil {
		return err
	}
	if _, err := w.kind.Write(kind); err != nil {
		return err
	}
	_, err := w.prov.Write(prov)
	return err
}

func (w *layerWriter) close() error {
	var first error
	for _, f := range []*os.File{w.dem, w.kind, w.prov} {
		if err := f.Close(); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// writeLayerFromMemory stores whole in-memory layers (after the global steps) as layer files.
func writeLayerFromMemory(l *layerFiles, dem []float32, kind, prov []uint8) error {
	w, err := newLayerWriter(l)
	if err != nil {
		return err
	}
	for r := 0; r < l.H; r++ {
		if err := w.writeRow(dem[r*l.W:(r+1)*l.W], kind[r*l.W:(r+1)*l.W], prov[r*l.W:(r+1)*l.W]); err != nil {
			w.close()
			return err
		}
	}
	return w.close()
}

// readLayerToMemory loads a whole layer.
func readLayerToMemory(l *layerFiles) (dem []float32, kind, prov []uint8, err error) {
	if err = l.open(); err != nil {
		return
	}
	defer l.close()
	if dem, err = l.dem.rect(0, 0, l.H, l.W); err != nil {
		return
	}
	if kind, err = l.kind.rect(0, 0, l.H, l.W, 0); err != nil {
		return
	}
	prov, err = l.prov.rect(0, 0, l.H, l.W, 0)
	return
}
