// Package preview renders a built world as a shaded relief/bathymetry map
// with the OSM coastline and tile grid, plus a self-contained HTML page.
package preview

import (
	"encoding/json"
	"fmt"
	"html"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/ocean-compiler/ocean/internal/gdal"
	"github.com/ocean-compiler/ocean/internal/validate"
	"github.com/ocean-compiler/ocean/internal/world"
)

// Render writes preview.png and preview.html into dir.
func Render(dir string) error {
	m, err := world.LoadManifest(dir)
	if err != nil {
		return err
	}
	N, S := m.Grid.TileCells, m.Grid.SamplesPerTile
	W, H := m.Grid.TilesX*N+1, m.Grid.TilesY*N+1
	z := make([]float64, W*H)
	for _, t := range m.Tiles {
		h, err := validate.ReadHeights(filepath.Join(dir, t.Height), m)
		if err != nil {
			return err
		}
		for i := 0; i < S; i++ {
			for j := 0; j < S; j++ {
				z[(t.Row*N+i)*W+t.Col*N+j] = h[i*S+j]
			}
		}
	}
	scale := max(1, 1400/W)
	img := image.NewRGBA(image.Rect(0, 0, (W-1)*scale, (H-1)*scale))
	at := func(c, r int) float64 { return z[min(max(r, 0), H-1)*W+min(max(c, 0), W-1)] }
	for y := 0; y < img.Rect.Dy(); y++ {
		for x := 0; x < img.Rect.Dx(); x++ {
			// Bilinear sample so upscaled previews are smooth.
			fx, fy := (float64(x)+0.5)/float64(scale)-0.5, (float64(y)+0.5)/float64(scale)-0.5
			c0, r0 := int(math.Floor(fx)), int(math.Floor(fy))
			ax, ay := fx-float64(c0), fy-float64(r0)
			v := at(c0, r0)*(1-ax)*(1-ay) + at(c0+1, r0)*ax*(1-ay) + at(c0, r0+1)*(1-ax)*ay + at(c0+1, r0+1)*ax*ay
			cc, rr := int(math.Round(fx)), int(math.Round(fy))
			// Hillshade, light from the north-west; slope in m/m.
			dzdx := (at(cc+1, rr) - at(cc-1, rr)) / (2 * m.Grid.CellM)
			dzdy := (at(cc, rr+1) - at(cc, rr-1)) / (2 * m.Grid.CellM)
			exag := 3.0
			shade := math.Max(0.35, math.Min(1.4, 0.9+exag*(-dzdx*0.7-dzdy*0.7)*0.9))
			img.Set(x, y, colorize(v, shade))
		}
	}
	// Shoreline from DEM zero crossing is implicit in the colour break; overlay OSM coastline.
	if f, ok := m.Files["coastline"]; ok {
		if err := drawCoast(img, filepath.Join(dir, f), m, scale, W, H); err != nil {
			return fmt.Errorf("coastline overlay: %w", err)
		}
	}
	// Tile grid.
	line := color.RGBA{255, 255, 255, 90}
	for c := 0; c <= m.Grid.TilesX; c++ {
		for y := 0; y < img.Rect.Dy(); y++ {
			img.Set(min(c*N*scale, img.Rect.Dx()-1), y, line)
		}
	}
	for r := 0; r <= m.Grid.TilesY; r++ {
		for x := 0; x < img.Rect.Dx(); x++ {
			img.Set(x, min(r*N*scale, img.Rect.Dy()-1), line)
		}
	}
	f, err := os.Create(filepath.Join(dir, "preview.png"))
	if err != nil {
		return err
	}
	if err := png.Encode(f, img); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	rep, err := validate.Run(dir)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "preview.html"), []byte(page(m, rep)), 0o644)
}

func colorize(v, shade float64) color.RGBA {
	var r, g, b float64
	if v >= 0 {
		// Land: green lowlands to brown to light grey.
		t := math.Min(v/200, 1)
		r, g, b = 96+t*100, 140-t*30, 80+t*10
		if v < 1.5 {
			r, g, b = 214, 200, 150 // beach/intertidal band
		}
	} else {
		d := math.Min(-v/100, 1)
		r, g, b = 120-d*105, 190-d*135, 235-d*105
	}
	cl := func(x float64) uint8 { return uint8(math.Max(0, math.Min(255, x*shade))) }
	return color.RGBA{cl(r), cl(g), cl(b), 255}
}

func drawCoast(img *image.RGBA, geojson string, m *world.Manifest, scale, W, H int) error {
	tmp := filepath.Join(os.TempDir(), fmt.Sprintf("ocean-coast-%d.geojson", os.Getpid()))
	defer os.Remove(tmp)
	if _, err := gdal.Run("ogr2ogr", "-f", "GeoJSON", "-t_srs", fmt.Sprintf("EPSG:%d", m.CRS.EPSG), tmp, geojson); err != nil {
		return err
	}
	b, err := os.ReadFile(tmp)
	if err != nil {
		return err
	}
	var fc struct {
		Features []struct {
			Geometry struct {
				Type        string          `json:"type"`
				Coordinates json.RawMessage `json:"coordinates"`
			} `json:"geometry"`
		} `json:"features"`
	}
	if err := json.Unmarshal(b, &fc); err != nil {
		return err
	}
	red := color.RGBA{225, 40, 40, 255}
	px := func(p []float64) (float64, float64) {
		return (p[0] - m.Grid.OriginE) / m.Grid.CellM * float64(scale), (m.Grid.OriginN - p[1]) / m.Grid.CellM * float64(scale)
	}
	for _, f := range fc.Features {
		var ln [][]float64
		if f.Geometry.Type != "LineString" || json.Unmarshal(f.Geometry.Coordinates, &ln) != nil {
			continue
		}
		for i := 1; i < len(ln); i++ {
			x0, y0 := px(ln[i-1])
			x1, y1 := px(ln[i])
			n := int(math.Max(math.Abs(x1-x0), math.Abs(y1-y0))) + 1
			for k := 0; k <= n; k++ {
				x, y := int(x0+(x1-x0)*float64(k)/float64(n)), int(y0+(y1-y0)*float64(k)/float64(n))
				if image.Pt(x, y).In(img.Rect) {
					img.Set(x, y, red)
				}
			}
		}
	}
	return nil
}

func page(m *world.Manifest, rep *validate.Report) string {
	var b strings.Builder
	b.WriteString(`<!doctype html><meta charset=utf-8><title>` + html.EscapeString(m.Name) + ` — Ocean Compiler preview</title>
<style>body{font:14px/1.5 system-ui,sans-serif;margin:0;background:#0d1b26;color:#dbe7ee}main{max-width:1500px;margin:auto;padding:16px}
img{max-width:100%;border:1px solid #345}pre{background:#091218;padding:12px;overflow:auto}h1{font-weight:600}
.leg span{display:inline-block;width:14px;height:14px;margin:0 4px -2px 12px}</style><main>`)
	fmt.Fprintf(&b, "<h1>%s</h1><p>%d tiles of %.0f m, %.0f m grid, EPSG:%d, build <code>%s</code>. Heights %.1f to %.1f m.</p>",
		html.EscapeString(m.Name), len(m.Tiles), m.Grid.TileSpanM, m.Grid.CellM, m.CRS.EPSG, m.BuildID[:12], m.Height.MinM, m.Height.MaxM)
	b.WriteString(`<img src="preview.png" alt="shaded relief and bathymetry"><p class=leg>
<span style="background:#6b8c50"></span>land <span style="background:#d6c896"></span>intertidal (&lt;1.5 m) <span style="background:#78bee9"></span>shallow water <span style="background:#0f3c80"></span>deep water <span style="background:#e12828"></span>OSM coastline <span style="background:#ffffff80"></span>tile edges</p>`)
	b.WriteString("<h2>Validation</h2><pre>" + html.EscapeString(rep.Format()) + "</pre><h2>Attribution</h2><ul>")
	for _, a := range m.Attribution {
		b.WriteString("<li>" + html.EscapeString(a) + "</li>")
	}
	b.WriteString("</ul></main>")
	return b.String()
}
