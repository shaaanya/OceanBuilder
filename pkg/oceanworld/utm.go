package oceanworld

import "math"

// WGS 84 / UTM forward and inverse projection (Krüger series to 6th order, Karney 2011). Pure Go, so a consumer
// can go from longitude/latitude to a world's metric grid without GDAL. Sub-millimetre inside a UTM zone.

const (
	wgsA  = 6378137.0
	wgsF  = 1 / 298.257223563
	utmK0 = 0.9996
)

type tmSeries struct {
	n, bigA    float64
	alpha, bet [7]float64
	delta      [7]float64
}

var tm = func() tmSeries {
	n := wgsF / (2 - wgsF)
	n2, n3, n4, n5, n6 := n*n, n*n*n, n*n*n*n, n*n*n*n*n, n*n*n*n*n*n
	s := tmSeries{n: n}
	s.bigA = wgsA / (1 + n) * (1 + n2/4 + n4/64 + n6/256)
	s.alpha = [7]float64{0,
		n/2 - 2*n2/3 + 5*n3/16 + 41*n4/180 - 127*n5/288 + 7891*n6/37800,
		13*n2/48 - 3*n3/5 + 557*n4/1440 + 281*n5/630 - 1983433*n6/1935360,
		61*n3/240 - 103*n4/140 + 15061*n5/26880 + 167603*n6/181440,
		49561*n4/161280 - 179*n5/168 + 6601661*n6/7257600,
		34729*n5/80640 - 3418889*n6/1995840,
		212378941 * n6 / 319334400}
	s.bet = [7]float64{0,
		n/2 - 2*n2/3 + 37*n3/96 - n4/360 - 81*n5/512 + 96199*n6/604800,
		n2/48 + n3/15 - 437*n4/1440 + 46*n5/105 - 1118711*n6/3870720,
		17*n3/480 - 37*n4/840 - 209*n5/4480 + 5569*n6/90720,
		4397*n4/161280 - 11*n5/504 - 830251*n6/7257600,
		4583*n5/161280 - 108847*n6/3991680,
		20648693 * n6 / 638668800}
	s.delta = [7]float64{0,
		2*n - 2*n2/3 - 2*n3 + 116*n4/45 + 26*n5/45 - 2854*n6/675,
		7*n2/3 - 8*n3/5 - 227*n4/45 + 2704*n5/315 + 2323*n6/945,
		56*n3/15 - 136*n4/35 - 1262*n5/105 + 73814*n6/2835,
		4279*n4/630 - 332*n5/35 - 399572*n6/14175,
		4174*n5/315 - 144838*n6/6237,
		601676 * n6 / 22275}
	return s
}()

// ZoneOf returns the UTM zone number and hemisphere (north = true) of an EPSG code 326zz / 327zz.
func ZoneOf(epsg int) (zone int, north bool, ok bool) {
	switch {
	case epsg >= 32601 && epsg <= 32660:
		return epsg - 32600, true, true
	case epsg >= 32701 && epsg <= 32760:
		return epsg - 32700, false, true
	}
	return 0, false, false
}

func centralMeridian(zone int) float64 { return float64(zone*6-183) * math.Pi / 180 }

// TM holds the parameters of a WGS 84 transverse Mercator grid: UTM zones are the special case K0 = 0.9996,
// FalseEasting = 500000, FalseNorthing = 0 (north) or 10000000 (south), Lon0 = the zone's central meridian.
// Worlds that do not fit one UTM zone use a local TM with their own Lon0 and K0 (manifest crs.kind "tmerc").
type TM struct {
	Lon0, K0, FalseEasting, FalseNorthing float64 // Lon0 in degrees
}

// UTMParams returns the TM parameters of a UTM zone.
func UTMParams(zone int, north bool) TM {
	p := TM{Lon0: centralMeridian(zone) * 180 / math.Pi, K0: utmK0, FalseEasting: 500000}
	if !north {
		p.FalseNorthing = 10000000
	}
	return p
}

// LonLatToUTM projects WGS 84 degrees to easting/northing in metres in the given zone (it does not choose one).
func LonLatToUTM(lon, lat float64, zone int, north bool) (e, n float64) {
	return LonLatToTM(lon, lat, UTMParams(zone, north))
}

// UTMToLonLat is the inverse of LonLatToUTM.
func UTMToLonLat(e, n float64, zone int, north bool) (lon, lat float64) {
	return TMToLonLat(e, n, UTMParams(zone, north))
}

// LonLatToTM projects WGS 84 degrees onto the transverse Mercator grid p. Longitudes are taken modulo 360 around Lon0,
// so a world centred on the antimeridian works with either 179.9 or -180.1.
func LonLatToTM(lon, lat float64, p TM) (e, n float64) {
	phi, dl := lat*math.Pi/180, wrapDeg(lon-p.Lon0)*math.Pi/180
	q := 2 * math.Sqrt(tm.n) / (1 + tm.n)
	t := math.Sinh(math.Atanh(math.Sin(phi)) - q*math.Atanh(q*math.Sin(phi)))
	xi0 := math.Atan2(t, math.Cos(dl))
	eta0 := math.Atanh(math.Sin(dl) / math.Sqrt(1+t*t))
	xi, eta := xi0, eta0
	for j := 1; j <= 6; j++ {
		xi += tm.alpha[j] * math.Sin(2*float64(j)*xi0) * math.Cosh(2*float64(j)*eta0)
		eta += tm.alpha[j] * math.Cos(2*float64(j)*xi0) * math.Sinh(2*float64(j)*eta0)
	}
	return p.FalseEasting + p.K0*tm.bigA*eta, p.FalseNorthing + p.K0*tm.bigA*xi
}

// TMToLonLat is the inverse of LonLatToTM; the longitude is returned in (-180, 180].
func TMToLonLat(e, n float64, p TM) (lon, lat float64) {
	xi, eta := (n-p.FalseNorthing)/(p.K0*tm.bigA), (e-p.FalseEasting)/(p.K0*tm.bigA)
	xi0, eta0 := xi, eta
	for j := 1; j <= 6; j++ {
		xi0 -= tm.bet[j] * math.Sin(2*float64(j)*xi) * math.Cosh(2*float64(j)*eta)
		eta0 -= tm.bet[j] * math.Cos(2*float64(j)*xi) * math.Sinh(2*float64(j)*eta)
	}
	chi := math.Asin(math.Sin(xi0) / math.Cosh(eta0))
	phi := chi
	for j := 1; j <= 6; j++ {
		phi += tm.delta[j] * math.Sin(2*float64(j)*chi)
	}
	lam := p.Lon0 + math.Atan2(math.Sinh(eta0), math.Cos(xi0))*180/math.Pi
	return wrapDeg(lam), phi * 180 / math.Pi
}

// wrapDeg maps an angle to (-180, 180].
func wrapDeg(d float64) float64 {
	d = math.Mod(d+180, 360)
	if d <= 0 {
		d += 360
	}
	return d - 180
}

// GroundScale is the point scale factor k of the grid p at lon/lat (series to fourth order in the longitude
// difference): 1 grid metre is 1/k ground metres in every direction at that point (TM is conformal). Distances over a
// larger area need the integral of 1/k along the path; areas scale by 1/k².
func GroundScale(lon, lat float64, p TM) float64 {
	phi := lat * math.Pi / 180
	d := wrapDeg(lon-p.Lon0) * math.Pi / 180 * math.Cos(phi)
	t := math.Tan(phi)
	eta2 := 0.00673949674228 * math.Cos(phi) * math.Cos(phi)
	return p.K0 * (1 + (1+eta2)*d*d/2 + (5-4*t*t+14*eta2)*d*d*d*d/24)
}
