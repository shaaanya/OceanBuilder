"""Run three `ocean build` processes at once on one fresh cache, repeatedly; every build must succeed with a byte-identical output tree and leave no temporary stage directories.

usage: concurrent_builds.py <ocean binary> <world.json (offline, e.g. over testdata/falmouth)> <scratch dir>
See docs/stage-cache.md."""
import sys, subprocess, os, hashlib, shutil
O=os.path.abspath(sys.argv[1]); CFG=os.path.abspath(sys.argv[2]); D=os.path.abspath(sys.argv[3])  # binary, offline world config, empty scratch dir
def tree(d):
    h=hashlib.sha256()
    for r,_,fs in sorted(os.walk(d)):
        for f in sorted(fs):
            p=os.path.join(r,f); h.update(os.path.relpath(p,d).encode()); h.update(open(p,'rb').read())
    return h.hexdigest()
ref=None
shutil.rmtree(D+"/cc",ignore_errors=True); os.makedirs(D+"/cc")
def run(cache,out): return subprocess.Popen([O,"build",CFG,"-cache",cache,"-out",out,"-lock","off","-force","-q"],stdout=subprocess.DEVNULL,stderr=subprocess.PIPE)
p=run(D+"/cc/refcache",D+"/cc/ref"); p.wait(); ref=tree(D+"/cc/ref"); print("ref",ref[:12])
bad=0
for trial in range(15):
    cache=D+f"/cc/cache{trial}"
    ps=[run(cache,D+f"/cc/out{trial}-{i}") for i in range(3)]
    rcs=[q.wait() for q in ps]
    errs=[q.stderr.read().decode().strip()[-100:] for q in ps]
    same=[tree(D+f"/cc/out{trial}-{i}")==ref if rcs[i]==0 else None for i in range(3)]
    left=[x for x in os.listdir(cache+"/stages") if ".tmp-" in x]
    print(f"trial {trial}: rc={rcs} identical={same} tmp_left={len(left)} errs={[e for e in errs if e]}")
