#!/usr/bin/env python3
# pylint: disable=C,R,W1514,W0621,W0611,W1510  # dense standalone script: style categories off, real errors on
"""ladder.py <ocean> <scratch dir> <world.json>...: measure real builds, cold cache then warm cache, per region.

For each config: wall time, peak resident memory of the ocean process (children included), peak bytes on disk under
the scratch dir (sampled every 0.25 s, so a short spike can be missed), downloaded bytes (new files in the cache that are
not stage rasters), output size, tile count, validate time and result. Prints one JSON line per run and a table.
POSIX only (uses resource.getrusage on children). Needs network for non-local configs. Refuses to start with less than
--min-free-gib of free disk space (default 2) so an experiment cannot fill the disk.
"""
import argparse, json, os, resource, shutil, subprocess, sys, threading, time


def du(path):
    n = 0
    for root, _, files in os.walk(path):
        for f in files:
            try: n += os.lstat(os.path.join(root, f)).st_size
            except OSError: pass
    return n


def run(cmd, watch):
    peak, stop = [0], threading.Event()
    def sample():
        while not stop.is_set():
            peak[0] = max(peak[0], du(watch)); time.sleep(0.25)
    th = threading.Thread(target=sample, daemon=True); th.start()
    t0 = time.time()
    with open(os.path.join(watch, "run.out"), "w+") as fo, open(os.path.join(watch, "run.err"), "w+") as fe:
        pr = subprocess.Popen(cmd, stdout=fo, stderr=fe)
        _, status, ru = os.wait4(pr.pid, 0)  # this child's own rusage, GDAL subprocesses it reaped included
        dt = time.time() - t0
        fo.seek(0); fe.seek(0); out, err = fo.read(), fe.read()
    stop.set(); th.join()
    os.remove(os.path.join(watch, "run.out")); os.remove(os.path.join(watch, "run.err"))
    # ru_maxrss is bytes on macOS and KiB on Linux
    rss = ru.ru_maxrss * (1 if sys.platform == "darwin" else 1024)
    p = subprocess.CompletedProcess(cmd, os.waitstatus_to_exitcode(status), out, err)
    return p, dt, rss, peak[0]


ap = argparse.ArgumentParser()
ap.add_argument("ocean"); ap.add_argument("scratch"); ap.add_argument("configs", nargs="+")
ap.add_argument("--min-free-gib", type=float, default=2.0)
a = ap.parse_args()
os.makedirs(a.scratch, exist_ok=True)
rows = []
for cfg in a.configs:
    name = json.load(open(cfg))["name"]
    base = os.path.join(a.scratch, name)
    if os.path.exists(base): shutil.rmtree(base)
    os.makedirs(base)
    cache, out = os.path.join(base, "cache"), os.path.join(base, "world")
    for phase in ("cold", "warm"):
        free = shutil.disk_usage(a.scratch).free / 2**30
        if free < a.min_free_gib:
            print("stop: %.1f GiB free < %.1f" % (free, a.min_free_gib)); sys.exit(3)
        p, dt, rss, peak = run([a.ocean, "build", cfg, "-out", out, "-cache", cache, "-force", "-q"], base)
        row = {"region": name, "phase": phase, "ok": p.returncode == 0, "seconds": round(dt, 1), "peak_rss_mib": round(rss / 2**20),
               "peak_disk_mib": round(peak / 2**20, 1)}
        if p.returncode != 0:
            row["error"] = (p.stderr or p.stdout).strip()[-300:]; rows.append(row); print(json.dumps(row)); break
        m = json.load(open(os.path.join(out, "world.manifest.json")))
        dl = sum(os.path.getsize(os.path.join(r, f)) for r, _, fs in os.walk(cache) for f in fs
                 if "stages" not in r.split(os.sep) and not f.endswith((".json", ".seal")))
        t0 = time.time(); v = subprocess.run([a.ocean, "validate", out], capture_output=True, text=True)
        row.update(cache_download_mib=round(dl / 2**20, 1), cache_total_mib=round(du(cache) / 2**20, 1), output_mib=round(du(out) / 2**20, 1),
                   cells_level0=m["stats"].get("cells_total") or m["grid"]["tiles_x"] * m["grid"]["tiles_y"] * m["grid"]["tile_cells"] ** 2,
                   tiles=len(m["tiles"]), levels=len(m["lod"]["levels"]), schema=m["schema_version"],
                   validate_seconds=round(time.time() - t0, 1), validate_exit=v.returncode,
                   result=[l for l in v.stdout.splitlines() if l.startswith("RESULT")][-1:] or None)
        rows.append(row); print(json.dumps(row), flush=True)
print("\n%-16s %-5s %7s %8s %9s %8s %8s %6s %6s %s" % ("region", "run", "sec", "rss MiB", "disk MiB", "dl MiB", "out MiB", "tiles", "val s", "validate"))
for r in rows:
    if r["ok"]: print("%-16s %-5s %7.1f %8d %9.1f %8.1f %8.1f %6d %6.1f %s" % (r["region"], r["phase"], r["seconds"], r["peak_rss_mib"], r["peak_disk_mib"], r["cache_download_mib"], r["output_mib"], r["tiles"], r["validate_seconds"], (r["result"] or ["?"])[0]))
    else: print("%-16s %-5s FAILED %s" % (r["region"], r["phase"], r["error"]))
