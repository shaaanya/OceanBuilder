"""Kill (SIGINT, SIGKILL) a real `ocean build` as soon as each kind of stage artifact appears in the cache, rebuild on the same cache, and require a byte-identical output tree.

usage: interrupt_resume.py <ocean binary> <world.json (offline, e.g. over testdata/falmouth)> <scratch dir>
See docs/stage-cache.md."""
import sys, subprocess, os, signal, time, hashlib, shutil, sys, json
O=os.path.abspath(sys.argv[1]); CFG=os.path.abspath(sys.argv[2]); D=os.path.abspath(sys.argv[3])  # binary, offline world config, empty scratch dir
def tree(d):
    h=hashlib.sha256()
    for r,_,fs in sorted(os.walk(d)):
        for f in sorted(fs):
            p=os.path.join(r,f); h.update(os.path.relpath(p,d).encode()); h.update(open(p,'rb').read())
    return h.hexdigest()
def build(cache,out,extra=()):
    return subprocess.Popen([O,"build",CFG,"-cache",cache,"-out",out,"-lock","off","-force","-q"],stdout=subprocess.DEVNULL,stderr=subprocess.PIPE,start_new_session=True)
def stages(cache):
    p=cache+"/stages"; return sorted(os.listdir(p)) if os.path.isdir(p) else []
shutil.rmtree(D+"/it",ignore_errors=True); os.makedirs(D+"/it")
t=time.time(); p=build(D+"/it/refc",D+"/it/ref"); p.wait(); ref=tree(D+"/it/ref"); print("clean build %.1fs rc=%d"%(time.time()-t,p.returncode),ref[:12],flush=True)
def wait_for(c,prefix,p,limit=300):
    t=time.time()
    while time.time()-t<limit and p.poll() is None:
        if any(x.startswith(prefix) for x in stages(c)): return True
        time.sleep(0.02)
    return False
for sig,name in ((signal.SIGINT,"SIGINT"),(signal.SIGKILL,"SIGKILL")):
    for trig in ("warp-or-bin:.bin","compose-","compose-.tmp","lod1-","lod2-"):
        key=trig
        c=D+f"/it/c-{name}-{trig.replace(':','_')}"; o=c.replace("/c-","/o-")
        p=build(c,o)
        def match(x):
            if trig=="warp-or-bin:.bin": return x.endswith(".bin")
            if trig=="compose-.tmp": return x.startswith("compose-") and ".tmp-" in x
            if trig=="compose-": return x.startswith("compose-") and ".tmp-" not in x
            return x.startswith(trig) and ".tmp-" not in x
        t=time.time(); hit=False
        while p.poll() is None and time.time()-t<300:
            if any(match(x) for x in stages(c)): hit=True; break
            time.sleep(0.005)
        if not hit: print(name,trig,"build ended before trigger"); p.wait(); continue
        snap=stages(c); p.send_signal(sig); p.wait(); err=p.stderr.read().decode()[-80:].strip()
        left=[x for x in stages(c) if ".tmp-" in x]
        p2=build(c,o); p2.wait()
        ok=(p2.returncode==0 and tree(o)==ref)
        print(f"{name} at {trig}: rc={p.returncode} entries_at_signal={len(snap)} tmp_left={len(left)} resume_rc={p2.returncode} identical={ok} err='{err}'",flush=True)
