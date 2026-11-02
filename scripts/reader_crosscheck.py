#!/usr/bin/env python3
# pylint: disable=C,R,W1514,W0621,W0611,W1510  # dense standalone script: style categories off, real errors on
"""reader_crosscheck.py <ocean binary> <world dir>: the Python reference reader (examples/python-reader) against `ocean sample`
at 40 random points on levels 0 and 2; elevations must agree within 1e-3 m (float32 rounding) and nodata must agree exactly."""
import json, os, random, subprocess, sys
sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "examples", "python-reader"))
import ocean_reader as R  # pylint: disable=import-error

ocean, world = sys.argv[1:3]
w, rnd = R.World(world), random.Random(7)
b = w.m["grid"]["bbox_wgs84"]
for lvl in (0, 2):
    if lvl >= w.levels: continue
    worst, n = 0.0, 0
    for _ in range(40):
        lon, lat = rnd.uniform(b[0] + .02, b[2] - .02), rnd.uniform(b[1] + .02, b[3] - .02)
        out = json.loads(subprocess.run([ocean, "sample", world, "-lonlat", "%.8f,%.8f" % (lon, lat), "-level", str(lvl), "-json"], capture_output=True, text=True, check=True).stdout)[0]
        mine = w.sample_lonlat(lon, lat, lvl)
        assert (not out["Valid"]) == (mine is None), "validity differs at %s,%s" % (lon, lat)
        if mine is not None:
            worst = max(worst, abs(mine - out["ElevationM"])); n += 1
    assert worst < 1e-3, "level %d differs by %g m" % (lvl, worst)
    print("level %d: %d points, worst |python - go| = %.1e m" % (lvl, n, worst))
