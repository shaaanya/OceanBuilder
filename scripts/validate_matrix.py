#!/usr/bin/env python3
# pylint: disable=C,R,W1514,W0621,W0611,W1510
"""validate_matrix.py <ocean> <ladder dir> <config>...: the acceptance checks on worlds built by scripts/ladder.py.

Per region (world in <ladder>/<name>/world, cache in <ladder>/<name>/cache): ocean validate exit code and warning codes,
provenance fields on every source, ATTRIBUTION.txt present, LOD report within the default thresholds, an independent
read (examples/python-reader: hashes, checkpoints, shared edges, parent samples, then 40 points per level against `ocean sample`),
and a deterministic rebuild (two more builds from the cache, byte-identical trees). Prints one JSON line per region."""
import hashlib, json, os, re, shutil, subprocess, sys

ocean, ladder = sys.argv[1:3]
root = os.path.dirname(os.path.abspath(__file__))


def tree_hash(d):
    h = hashlib.sha256()
    for r, _, fs in sorted(os.walk(d)):
        for f in sorted(fs):
            p = os.path.join(r, f)
            h.update(os.path.relpath(p, d).encode()); h.update(hashlib.sha256(open(p, "rb").read()).digest())
    return h.hexdigest()[:16]


for cfg in sys.argv[3:]:
    name = json.load(open(cfg))["name"]
    base = os.path.join(ladder, name); world, cache = os.path.join(base, "world"), os.path.join(base, "cache")
    row = {"region": name}
    v = subprocess.run([ocean, "validate", world], capture_output=True, text=True)
    row["validate_exit"] = v.returncode
    row["warnings"] = sorted(set(re.findall(r"^\s+\[([a-z0-9-]+)\]", v.stdout.split("WARNINGS:")[-1].split("INFOS:")[0], re.M))) if "WARNINGS:" in v.stdout else []
    m = json.load(open(os.path.join(world, "world.manifest.json")))
    row["schema"] = m["schema_version"]; row["crs"] = m["crs"].get("kind", "utm")
    need = ("source", "url", "retrieved_at", "sha256", "bytes", "license", "attribution")
    row["sources"] = len(m["sources"]); row["provenance_complete"] = all(all(s.get(k) not in (None, "", 0) for k in need) for s in m["sources"])
    a = os.path.join(world, "ATTRIBUTION.txt"); row["attribution_bytes"] = os.path.getsize(a) if os.path.exists(a) else 0
    lr = subprocess.run([ocean, "lod-report", world], capture_output=True, text=True); row["lod_report_exit"] = lr.returncode
    rd = subprocess.run([sys.executable, "-I", os.path.join(root, "..", "examples", "python-reader", "ocean_reader.py"), world, "--check"], capture_output=True, text=True)
    row["python_reader_check"] = rd.stdout.strip().splitlines()[0] if rd.stdout else rd.stderr.strip()[-120:]
    cc = subprocess.run([sys.executable, "-I", os.path.join(root, "reader_crosscheck.py"), ocean, world], capture_output=True, text=True)
    row["python_vs_ocean_sample"] = cc.stdout.strip().replace("\n", "; ") if cc.returncode == 0 else "FAILED " + cc.stderr.strip()[-150:]
    hashes = []
    for i in (1, 2):
        out = os.path.join(base, "rebuild%d" % i)
        subprocess.run([ocean, "build", cfg, "-out", out, "-cache", cache, "-force", "-q"], capture_output=True, text=True, check=True)
        hashes.append(tree_hash(out))
        shutil.rmtree(out)  # the tree hash is what is compared; keep the disk free
    row["deterministic_rebuild"] = hashes[0] == hashes[1]; row["tree_hash"] = hashes[0]
    row["first_vs_rebuild_same"] = tree_hash(world) == hashes[0]
    print(json.dumps(row), flush=True)
