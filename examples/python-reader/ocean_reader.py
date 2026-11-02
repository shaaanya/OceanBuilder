#!/usr/bin/env python3
# pylint: disable=C,R,W1514,W0621,W0611,W1510  # dense standalone script: style categories off, real errors on
"""Reference consumer of an OceanBuilder world (schema 2 and 3) using only the Python standard library.

It reads world.manifest.json, the little-endian float32 height tiles and the 8-bit PNG layers, projects lon/lat to the
world's grid (WGS 84 transverse Mercator, Kruger series, UTM zones and local tmerc worlds alike), and answers
elevation / depth class / source queries. It shares no code with OceanBuilder; docs/formats/world-v2.md is its only spec.

    python3 ocean_reader.py WORLD --lonlat -5.09,50.17 [--level 0]
    python3 ocean_reader.py WORLD --check       # hashes, checkpoints, edge sharing, LOD parent samples
"""
import argparse, hashlib, json, math, os, struct, sys, zlib

SUPPORTED_SCHEMAS = (2, 3)
A, F = 6378137.0, 1 / 298.257223563


def _kruger():
    n = F / (2 - F)
    n2, n3, n4, n5, n6 = n**2, n**3, n**4, n**5, n**6
    bigA = A / (1 + n) * (1 + n2 / 4 + n4 / 64 + n6 / 256)
    alpha = [0,
             n / 2 - 2 * n2 / 3 + 5 * n3 / 16 + 41 * n4 / 180 - 127 * n5 / 288 + 7891 * n6 / 37800,
             13 * n2 / 48 - 3 * n3 / 5 + 557 * n4 / 1440 + 281 * n5 / 630 - 1983433 * n6 / 1935360,
             61 * n3 / 240 - 103 * n4 / 140 + 15061 * n5 / 26880 + 167603 * n6 / 181440,
             49561 * n4 / 161280 - 179 * n5 / 168 + 6601661 * n6 / 7257600,
             34729 * n5 / 80640 - 3418889 * n6 / 1995840,
             212378941 * n6 / 319334400]
    return n, bigA, alpha


_N, _BIGA, _ALPHA = _kruger()


def _wrap(d):
    d = math.fmod(d + 180, 360)
    if d <= 0:
        d += 360
    return d - 180


def lonlat_to_grid(lon, lat, crs):
    """WGS 84 degrees -> (easting, northing) in the world's grid. crs is the manifest's crs object."""
    if crs.get("kind", "utm") == "utm":
        epsg = crs["epsg"]
        zone = epsg % 100
        lon0, k0, fe, fn = zone * 6 - 183, 0.9996, 500000.0, (0.0 if epsg < 32700 else 10000000.0)
    else:
        lon0, k0 = crs["lon_0"], crs["k_0"]
        fe, fn = crs.get("false_easting", 0.0), crs.get("false_northing", 0.0)  # zero values may be omitted
    phi, dl = math.radians(lat), math.radians(_wrap(lon - lon0))
    q = 2 * math.sqrt(_N) / (1 + _N)
    t = math.sinh(math.atanh(math.sin(phi)) - q * math.atanh(q * math.sin(phi)))
    xi0, eta0 = math.atan2(t, math.cos(dl)), math.atanh(math.sin(dl) / math.sqrt(1 + t * t))
    xi, eta = xi0, eta0
    for j in range(1, 7):
        xi += _ALPHA[j] * math.sin(2 * j * xi0) * math.cosh(2 * j * eta0)
        eta += _ALPHA[j] * math.cos(2 * j * xi0) * math.sinh(2 * j * eta0)
    return fe + k0 * _BIGA * eta, fn + k0 * _BIGA * xi


def read_png_gray8(path, S):
    """Decode a non-interlaced 8-bit greyscale PNG into S*S bytes (row-major, rows north to south)."""
    data = open(path, "rb").read()
    assert data[:8] == b"\x89PNG\r\n\x1a\n", path + ": not a PNG"
    pos, idat, w = 8, b"", None
    while pos < len(data):
        n, typ = struct.unpack(">I4s", data[pos:pos + 8])
        body = data[pos + 8:pos + 8 + n]
        if typ == b"IHDR":
            w, h, depth, ctype, _, _, inter = struct.unpack(">IIBBBBB", body)
            assert (w, h, depth, ctype, inter) == (S, S, 8, 0, 0), "%s: expected %dx%d 8-bit grey" % (path, S, S)
        elif typ == b"IDAT":
            idat += body
        pos += 12 + n
    raw = zlib.decompress(idat)
    out, prev = bytearray(), bytearray(S)
    for r in range(S):
        ft, line = raw[r * (S + 1)], bytearray(raw[r * (S + 1) + 1:(r + 1) * (S + 1)])
        for i in range(S):
            a = line[i - 1] if i else 0
            b = prev[i]
            c = prev[i - 1] if i else 0
            if ft == 1: line[i] = (line[i] + a) & 255
            elif ft == 2: line[i] = (line[i] + b) & 255
            elif ft == 3: line[i] = (line[i] + (a + b) // 2) & 255
            elif ft == 4:
                p = a + b - c
                pa, pb, pc = abs(p - a), abs(p - b), abs(p - c)
                line[i] = (line[i] + (a if pa <= pb and pa <= pc else b if pb <= pc else c)) & 255
            elif ft != 0: raise ValueError("bad PNG filter")
        out += line
        prev = line
    return bytes(out)


class World:
    def __init__(self, root):
        self.root = root
        self.m = json.load(open(os.path.join(root, "world.manifest.json")))
        if self.m["schema_version"] not in SUPPORTED_SCHEMAS:
            raise SystemExit("unsupported schema_version %r (this reader knows %s)" % (self.m["schema_version"], SUPPORTED_SCHEMAS))
        g = self.m["grid"]
        self.S, self.cell, self.span = g["samples_per_tile"], g["cell_m"], g["tile_span_m"]
        self.nodata = self.m["height_encoding"]["nodata"]
        self.tiles = {(t["level"], t["x"], t["y"]): t for t in self.m["tiles"]}
        self.levels = len(self.m["lod"]["levels"])
        self._cache = {}

    def heights(self, t):
        if t["id"] not in self._cache:
            raw = open(os.path.join(self.root, t["height"]), "rb").read()
            if len(raw) != self.S * self.S * 4:
                raise ValueError(t["id"] + ": wrong height file size")
            self._cache[t["id"]] = struct.unpack("<%df" % (self.S * self.S), raw)
        return self._cache[t["id"]]

    def layer(self, t, name):
        return read_png_gray8(os.path.join(self.root, t[name]), self.S)

    def tile_at(self, E, N, level):
        sp = self.span * 2 ** level
        return self.tiles.get((level, math.floor(E / sp), math.floor(N / sp)))

    def sample(self, E, N, level=0):
        """Bilinear elevation at a grid position. Returns None when the tile does not exist (outside the world, or
        a level>0 tile with no valid sample) or any of the four vertices is nodata."""
        sp, c, S = self.span * 2 ** level, self.cell * 2 ** level, self.S
        x, y = math.floor(E / sp), math.floor(N / sp)
        t = self.tiles.get((level, x, y))
        if t is None:
            return None
        u, v = (E - x * sp) / c, ((y + 1) * sp - N) / c
        j, i = min(int(u), S - 2), min(int(v), S - 2)
        fu, fv = u - j, v - i
        h = self.heights(t)
        q = (h[i * S + j], h[i * S + j + 1], h[(i + 1) * S + j], h[(i + 1) * S + j + 1])
        if any(abs(s - self.nodata) < 0.5 for s in q):
            return None
        return (q[0] * (1 - fu) + q[1] * fu) * (1 - fv) + (q[2] * (1 - fu) + q[3] * fu) * fv

    def sample_lonlat(self, lon, lat, level=0):
        E, N = lonlat_to_grid(lon, lat, self.m["crs"])
        return self.sample(E, N, level)

    def nearest_codes(self, E, N, level=0):
        """(depth class, source code) of the nearest vertex, or None."""
        sp, c, S = self.span * 2 ** level, self.cell * 2 ** level, self.S
        x, y = math.floor(E / sp), math.floor(N / sp)
        t = self.tiles.get((level, x, y))
        if t is None:
            return None
        j = min(round((E - x * sp) / c), S - 1)
        i = min(round(((y + 1) * sp - N) / c), S - 1)
        return self.layer(t, "depth_class")[i * S + j], self.layer(t, "source")[i * S + j]

    def check(self):
        """Independent conformance checks; returns a list of failure strings (empty = pass)."""
        bad, S = [], self.S
        for t in self.m["tiles"]:
            for key, field in (("height", "height"), ("depth_class", "depth_class"), ("shore_mask", "shore_mask"), ("source", "source"), ("dataset", "dataset")):
                if field in t:
                    d = hashlib.sha256(open(os.path.join(self.root, t[field]), "rb").read()).hexdigest()
                    if d != t["sha256"][key]:
                        bad.append("%s: %s hash differs" % (t["id"], key))
        for cp in self.m.get("checkpoints", []):
            got = self.sample(cp["easting"], cp["northing"], 0)
            if got is None or abs(got - cp["elevation_m"]) > 1e-3:
                bad.append("checkpoint %s: expected %.3f got %r" % (cp["name"], cp["elevation_m"], got))
        for (lv, x, y), t in self.tiles.items():  # shared edges: east column of (x,y) == west column of (x+1,y)
            e = self.tiles.get((lv, x + 1, y))
            if e:
                a, b = self.heights(t), self.heights(e)
                if any(a[i * S + S - 1] != b[i * S] for i in range(S)):
                    bad.append("%s / %s: east edge differs" % (t["id"], e["id"]))
        for (lv, x, y), t in self.tiles.items():  # parent vertex == child vertex at the same position (decimation only)
            if lv + 1 < self.levels and self.m["lod"].get("method", "vertex-decimation") == "vertex-decimation":
                p = self.tiles.get((lv + 1, x >> 1, y >> 1))
                if p is None:
                    bad.append("%s: parent tile missing" % t["id"]); continue
                ph, ch = self.heights(p), self.heights(t)
                oj, oi = (x & 1) * (S - 1) // 2, (1 - (y & 1)) * (S - 1) // 2
                for i in range(0, S, 2):
                    for j in range(0, S, 2):
                        if ph[(oi + i // 2) * S + oj + j // 2] != ch[i * S + j]:
                            bad.append("%s: parent sample differs at %d,%d" % (t["id"], i, j)); break
                    else: continue
                    break
        return bad


if __name__ == "__main__":
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("world"); ap.add_argument("--lonlat"); ap.add_argument("--level", type=int, default=0); ap.add_argument("--check", action="store_true")
    a = ap.parse_args()
    w = World(a.world)
    if a.check:
        bad = w.check()
        print("%d tiles, %d levels, schema %d: %s" % (len(w.tiles), w.levels, w.m["schema_version"], "OK" if not bad else "%d problems" % len(bad)))
        for b in bad[:20]: print(" ", b)
        sys.exit(1 if bad else 0)
    if a.lonlat:
        lon, lat = map(float, a.lonlat.split(","))
        E, N = lonlat_to_grid(lon, lat, w.m["crs"])
        h = w.sample(E, N, a.level)
        codes = w.nearest_codes(E, N, a.level)
        print(json.dumps({"lon": lon, "lat": lat, "easting": E, "northing": N, "level": a.level, "elevation_m": h,
                          "depth_class": codes[0] if codes else None, "source_code": codes[1] if codes else None}))
