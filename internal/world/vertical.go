package world

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/ocean-compiler/ocean/internal/fetch"
	"github.com/ocean-compiler/ocean/internal/gdal"
)

// Vertical references. Elevations from different sources are only comparable when they share a vertical datum,
// and the offset between datums can be metres (tidal range) to tens of metres (geoid vs ellipsoid). The builder
// therefore never guesses: every dataset declares its datum, a transformation is applied only where PROJ has a
// published grid-based operation (checked against ballpark fall-backs), a user may supply a documented constant
// offset, and everything else is refused unless the user explicitly accepts a mixed result (recorded in the manifest).

// VerticalRef is a dataset's declared vertical reference.
type VerticalRef struct {
	Datum string `json:"datum"` // see vertDatums; "unknown" is allowed and means exactly that
	// OffsetM is added to the dataset's values to express them in the world's target datum. It is the user's
	// statement, not a computed transformation; OffsetSource (a citation or measurement) is then mandatory.
	OffsetM      *float64 `json:"offset_m,omitempty"`
	OffsetSource string   `json:"offset_source,omitempty"`
}

// VerticalTarget is the vertical reference of the built world.
type VerticalTarget struct {
	Datum      string `json:"datum"`
	AllowMixed bool   `json:"allow_mixed,omitempty"` // build even when datasets use different or unknown references
}

type vdatum struct {
	Name  string
	EPSG  int    // EPSG vertical (or geographic-3D for the ellipsoid) CRS code, 0 when no transformable definition exists
	Class string // geoid | ellipsoid | tidal | sea-level | mosaic | unknown
	Note  string
}

var vertDatums = []vdatum{
	{"EGM96", 5773, "geoid", "EGM96 geoid height (EPSG:5773), NGA"},
	{"EGM2008", 3855, "geoid", "EGM2008 geoid height (EPSG:3855), NGA"},
	{"WGS84-ellipsoid", 4979, "ellipsoid", "height above the WGS 84 ellipsoid (EPSG:4979)"},
	{"MSL", 0, "sea-level", "mean sea level of unspecified epoch and realisation: not the same as a geoid model"},
	{"NAVD88", 0, "geoid", "North American Vertical Datum 1988: geoid-based, but no verified grid operation is wired in"},
	{"MLLW", 0, "tidal", "mean lower low water"}, {"MLW", 0, "tidal", "mean low water"}, {"MHW", 0, "tidal", "mean high water"},
	{"MHHW", 0, "tidal", "mean higher high water"}, {"LAT", 0, "tidal", "lowest astronomical tide"}, {"HAT", 0, "tidal", "highest astronomical tide"},
	{"GMRT-mosaic", 0, "mosaic", "GMRT: a mosaic of contributed grids with heterogeneous references, nominally sea level"},
	{"unknown", 0, "unknown", "not documented by the source"},
}

func lookupDatum(name string) (vdatum, bool) {
	for _, d := range vertDatums {
		if strings.EqualFold(d.Name, name) {
			return d, true
		}
	}
	return vdatum{}, false
}

// VerticalDatumNames lists the accepted datum names (for help and errors).
func VerticalDatumNames() []string {
	var n []string
	for _, d := range vertDatums {
		n = append(n, d.Name)
	}
	return n
}

// GridRef is a transformation grid used by an applied vertical operation.
type GridRef struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
	Bytes  int64  `json:"bytes"`
}

// VerticalApplied records how a dataset's values were brought to the world's datum.
type VerticalApplied struct {
	Kind         string    `json:"kind"` // identity | proj-grid | user-offset
	From         string    `json:"from"`
	To           string    `json:"to"`
	Pipeline     string    `json:"pipeline,omitempty"`
	Grids        []GridRef `json:"grids,omitempty"`
	ProjVersion  string    `json:"proj_version,omitempty"`
	OffsetM      *float64  `json:"offset_m,omitempty"`
	OffsetSource string    `json:"offset_source,omitempty"`
	Verified     bool      `json:"verified"` // true only for identity and for a published grid operation resolved by PROJ; a user offset is a claim
}

var (
	projOnce   sync.Once
	projVer    string
	projSearch []string
)

func projInfo() (string, []string) {
	projOnce.Do(func() {
		projVer = "unavailable"
		// `proj` without arguments prints its usage and exits non-zero; the release is in the error text.
		if _, err := gdal.Run("proj"); err != nil {
			if m := regexp.MustCompile(`Rel\. ([0-9.]+)`).FindStringSubmatch(err.Error()); m != nil {
				projVer = m[1]
			}
		}
		if out, err := gdal.Run("projinfo", "--searchpaths"); err == nil {
			for _, l := range strings.Split(out, "\n") {
				if l = strings.TrimSpace(l); l != "" {
					projSearch = append(projSearch, l)
				}
			}
		}
	})
	return projVer, projSearch
}

var gridHashes sync.Map

// gridFile finds a PROJ grid by name in the search path and hashes it (the hash is part of the input key: a
// different grid release is a different transformation).
func gridFile(name string) (GridRef, error) {
	_, paths := projInfo()
	for _, p := range paths {
		f := filepath.Join(p, name)
		fi, err := os.Stat(f)
		if err != nil || fi.IsDir() {
			continue
		}
		if h, ok := gridHashes.Load(f); ok {
			return GridRef{Name: name, SHA256: h.(string), Bytes: fi.Size()}, nil
		}
		sum, err := fetch.SHA256File(f)
		if err != nil {
			return GridRef{}, err
		}
		gridHashes.Store(f, sum)
		return GridRef{Name: name, SHA256: sum, Bytes: fi.Size()}, nil
	}
	return GridRef{}, fmt.Errorf("transformation grid %s not found in the PROJ data path %v (install proj-data; OceanBuilder runs PROJ with network access disabled so builds are reproducible)", name, paths)
}

var gridsRE = regexp.MustCompile(`\+grids=([^\s]+)`)

func compound(h, v int) string {
	if v == 4979 {
		return "EPSG:4979"
	}
	return fmt.Sprintf("EPSG:%d+%d", h, v)
}

// planVertical decides how values referenced to `from` get to `to`, or why they cannot.
func planVertical(from VerticalRef, to string, horizEPSG int) (*VerticalApplied, error) {
	fd, ok := lookupDatum(from.Datum)
	if !ok {
		return nil, fmt.Errorf("unknown vertical datum %q (known: %s)", from.Datum, strings.Join(VerticalDatumNames(), ", "))
	}
	td, ok := lookupDatum(to)
	if !ok {
		return nil, fmt.Errorf("unknown target vertical datum %q (known: %s)", to, strings.Join(VerticalDatumNames(), ", "))
	}
	ap := &VerticalApplied{From: fd.Name, To: td.Name}
	if from.OffsetM != nil {
		if from.OffsetSource == "" {
			return nil, fmt.Errorf("vertical offset_m for datum %s needs offset_source (where does the %.3g m come from?)", fd.Name, *from.OffsetM)
		}
		ap.Kind, ap.OffsetM, ap.OffsetSource = "user-offset", from.OffsetM, from.OffsetSource
		return ap, nil
	}
	if fd.Name == td.Name {
		ap.Kind, ap.Verified = "identity", true
		return ap, nil
	}
	if fd.EPSG == 0 || td.EPSG == 0 {
		why := fmt.Sprintf("no verified transformation from %s (%s) to %s (%s)", fd.Name, fd.Class, td.Name, td.Class)
		if fd.Class == "tidal" || td.Class == "tidal" {
			why += ": tidal datums vary by location and need a tide model (for example NOAA VDatum) that OceanBuilder does not include"
		}
		return nil, fmt.Errorf("%s. Convert the data outside OceanBuilder, or declare a documented constant with vertical.offset_m and offset_source, or accept a mixed result with vertical.allow_mixed", why)
	}
	if horizEPSG == 0 && fd.EPSG != 4979 {
		return nil, fmt.Errorf("cannot transform %s to %s: the source has no EPSG horizontal CRS to combine with the vertical one", fd.Name, td.Name)
	}
	src, dst := compound(horizEPSG, fd.EPSG), compound(horizEPSG, td.EPSG)
	out, err := gdal.Run("projinfo", "-s", src, "-t", dst, "-o", "PROJ", "-q")
	if err != nil {
		return nil, fmt.Errorf("PROJ cannot relate %s to %s: %w", src, dst, err)
	}
	if strings.Contains(out, "+proj=noop") || !strings.Contains(out, "vgridshift") && !strings.Contains(out, "geoid") {
		return nil, fmt.Errorf("PROJ offers only a ballpark (no-op) operation from %s to %s; refusing to pretend the heights are comparable", fd.Name, td.Name)
	}
	ap.Kind, ap.Verified, ap.Pipeline = "proj-grid", true, strings.Join(strings.Fields(out), " ")
	ap.ProjVersion, _ = projInfo()
	seen := map[string]bool{}
	for _, m := range gridsRE.FindAllStringSubmatch(out, -1) {
		for _, name := range strings.Split(m[1], ",") {
			name = strings.TrimPrefix(name, "@")
			if seen[name] {
				continue
			}
			seen[name] = true
			g, err := gridFile(name)
			if err != nil {
				return nil, err
			}
			ap.Grids = append(ap.Grids, g)
		}
	}
	sort.Slice(ap.Grids, func(i, j int) bool { return ap.Grids[i].Name < ap.Grids[j].Name })
	return ap, nil
}

// key identifies the operation for the stage cache.
func (a *VerticalApplied) key() string {
	h := sha256.New()
	fmt.Fprint(h, a.Kind, a.From, a.To, a.Pipeline, a.ProjVersion)
	for _, g := range a.Grids {
		fmt.Fprint(h, g.Name, g.SHA256)
	}
	if a.OffsetM != nil {
		fmt.Fprint(h, *a.OffsetM)
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

type rasterGeom struct {
	W, H   int
	GT     [6]float64
	EPSG   int // horizontal EPSG code, 0 when the file does not state one
	NoData *float64
	// Compound is true when the file's CRS includes a vertical component. A warp to a horizontal-only CRS would then
	// silently convert the heights to ellipsoidal ones, so such files are warped with an explicit horizontal -s_srs.
	Compound bool
}

// inspectRaster reads size, geotransform, horizontal EPSG (0 when absent) and nodata of a raster.
func inspectRaster(path string) (rasterGeom, error) {
	out, err := gdal.Run("gdalinfo", "-json", path)
	if err != nil {
		return rasterGeom{}, err
	}
	var info struct {
		Size []int     `json:"size"`
		GT   []float64 `json:"geoTransform"`
		STAC struct {
			EPSG any `json:"proj:epsg"`
		} `json:"stac"`
		CS struct {
			WKT string `json:"wkt"`
		} `json:"coordinateSystem"`
		Bands []struct {
			NoData any `json:"noDataValue"`
		} `json:"bands"`
	}
	if err := json.Unmarshal([]byte(out), &info); err != nil || len(info.Size) != 2 || len(info.GT) != 6 {
		return rasterGeom{}, fmt.Errorf("gdalinfo %s: unreadable size or geotransform", path)
	}
	g := rasterGeom{W: info.Size[0], H: info.Size[1]}
	copy(g.GT[:], info.GT)
	g.Compound = strings.Contains(info.CS.WKT, "VERT_CS") || strings.Contains(info.CS.WKT, "VERTCRS") || strings.HasPrefix(strings.TrimSpace(info.CS.WKT), "COMPD_CS") || strings.HasPrefix(strings.TrimSpace(info.CS.WKT), "COMPOUNDCRS")
	if e, ok := info.STAC.EPSG.(float64); ok {
		g.EPSG = int(e)
	}
	if len(info.Bands) > 0 {
		if v, ok := info.Bands[0].NoData.(float64); ok {
			g.NoData = &v
		}
	}
	return g, nil
}

// shiftRaster writes src with a proj-grid operation applied, on the identical pixel grid, so the later
// horizontal warp sees the same cells it would have seen without the shift.
func (a *VerticalApplied) shiftRaster(src, dst string, geom rasterGeom) error {
	if geom.GT[2] != 0 || geom.GT[4] != 0 || geom.GT[5] >= 0 {
		return fmt.Errorf("%s: a vertical transformation needs a north-up raster", src)
	}
	fd, _ := lookupDatum(a.From)
	td, _ := lookupDatum(a.To)
	xmax, ymin := geom.GT[0]+float64(geom.W)*geom.GT[1], geom.GT[3]+float64(geom.H)*geom.GT[5]
	args := []string{"-q", "-overwrite", "-s_srs", compound(geom.EPSG, fd.EPSG), "-t_srs", compound(geom.EPSG, td.EPSG), "-r", "near", "-ot", "Float32",
		"-te", fmt.Sprint(geom.GT[0]), fmt.Sprint(ymin), fmt.Sprint(xmax), fmt.Sprint(geom.GT[3]),
		"-ts", fmt.Sprint(geom.W), fmt.Sprint(geom.H), "-of", "GTiff", "-co", "COMPRESS=DEFLATE"}
	if geom.NoData != nil {
		args = append(args, "-srcnodata", fmt.Sprint(*geom.NoData), "-dstnodata", fmt.Sprint(*geom.NoData))
	}
	tmp := dst + ".compound.tif"
	defer os.Remove(tmp)
	if _, err := gdal.Run("gdalwarp", append(args, src, tmp)...); err != nil {
		return err
	}
	// The result is in the target vertical datum; drop the vertical part of its CRS so that the world warp does not
	// convert it again (GDAL treats "no vertical CRS" as ellipsoidal height).
	_, err := gdal.Run("gdal_translate", "-q", "-a_srs", fmt.Sprintf("EPSG:%d", geom.EPSG), "-co", "COMPRESS=DEFLATE", tmp, dst)
	return err
}

// resolveVerticalSet decides the world's vertical datum and the operation for each dataset. It is the refusal point
// of the vertical model: datasets that cannot be shown to share a reference do not get composed.
func resolveVerticalSet(cfg *Config, dss []*dataset, opt Options) (VerticalInfo, error) {
	info := VerticalInfo{Unit: "metre"}
	target, allowMixed := "", false
	if cfg.Vertical != nil {
		target, allowMixed = cfg.Vertical.Datum, cfg.Vertical.AllowMixed
	}
	if target == "" {
		first, same := dss[0].datumName(), true
		for _, d := range dss {
			same = same && d.datumName() == first
		}
		switch {
		case len(dss) == 1 || same && first != "unknown":
			target = first
		case allowMixed:
			target, info.Mixed = "mixed", true
		default:
			var l []string
			for _, d := range dss {
				l = append(l, d.spec.ID+"="+d.datumName())
			}
			return info, fmt.Errorf("datasets do not share a verified vertical reference (%s). Composing them would mix heights that can differ by metres. Set vertical.datum to a target with supported transformations (%s), give a documented vertical.offset_m per dataset, or accept the mix explicitly with vertical.allow_mixed",
				strings.Join(l, ", "), strings.Join(VerticalDatumNames(), ", "))
		}
	}
	info.Datum = target
	for _, d := range dss {
		if target == "mixed" {
			if d.vref.OffsetM != nil {
				return info, fmt.Errorf("source %s: vertical.offset_m needs a target datum; with allow_mixed and no vertical.datum nothing is converted", d.spec.ID)
			}
			d.vapplied = &VerticalApplied{Kind: "none", From: d.datumName(), To: "mixed"}
			continue
		}
		horiz := 4326
		if d.spec.Type == "geotiff" {
			g, err := inspectRaster(d.vals[0].File)
			if err != nil {
				return info, err
			}
			horiz = g.EPSG
			if g.Compound {
				if g.EPSG == 0 {
					return info, fmt.Errorf("source %s: the file has a compound CRS (horizontal + vertical) without an EPSG horizontal part; re-project or strip its vertical CRS first", d.spec.ID)
				}
				d.srcSRS = fmt.Sprintf("EPSG:%d", g.EPSG)
				opt.logf("source %s embeds a vertical CRS; it is ignored for warping and the declared datum %s is used", d.spec.ID, d.datumName())
			}
		}
		ap, err := planVertical(d.vref, target, horiz)
		if err != nil {
			if !allowMixed {
				return info, fmt.Errorf("source %s: %w", d.spec.ID, err)
			}
			ap, info.Mixed = &VerticalApplied{Kind: "none", From: d.datumName(), To: target}, true
			opt.logf("WARNING: source %s: %v (accepted by vertical.allow_mixed; heights are NOT converted)", d.spec.ID, err)
		}
		d.vapplied = ap
		switch ap.Kind {
		case "user-offset":
			d.offsetM = *ap.OffsetM
		case "proj-grid":
			for i, p := range d.vals {
				g, err := inspectRaster(p.File)
				if err != nil {
					return info, err
				}
				dst := filepath.Join(opt.CacheDir, "stages", "vshift-"+p.SHA256[:16]+"-"+ap.key()+".tif")
				if _, err := os.Stat(dst); err != nil {
					if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
						return info, err
					}
					if err := ap.shiftRaster(p.File, dst+".tmp.tif", g); err != nil {
						return info, fmt.Errorf("source %s: %w", d.spec.ID, err)
					}
					if err := os.Rename(dst+".tmp.tif", dst); err != nil {
						return info, err
					}
				}
				d.valSrc[i] = dst
			}
		}
	}
	if len(dss) == 1 && !info.Mixed {
		if dd, ok := lookupDatum(target); ok {
			info.Note = dd.Note
		}
	}
	switch {
	case info.Mixed:
		info.Note = "Datasets with different or unverified vertical references were combined on request (vertical.allow_mixed); see datasets[].vertical for what was and was not converted. Heights are not comparable across datasets to better than their datum differences."
	case target == "unknown":
		info.Note = "The vertical reference of the input is not documented. Zero is treated as the land/water boundary; no claim is made about the datum."
	}
	if info.Note == "" {
		info.Note = "All datasets are expressed in " + target + "."
	}
	return info, nil
}
