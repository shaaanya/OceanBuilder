"""Independent geographic verification of an OceanBuilder world (python pyproj+numpy+gdal CLI; shares no code with the Go tool)."""
import json,sys,subprocess,random,glob,math
import numpy as np
from pyproj import Transformer, Geod
out,raw=sys.argv[1],sys.argv[2]
m=json.load(open(out+"/world.manifest.json")); g=m['grid']; N=g['tile_cells']; S=g['samples_per_tile']; cell=g['cell_m']
crs=m['crs']; epsg=f"EPSG:{crs['epsg']}" if crs.get('epsg') else crs['proj4']
tf=Transformer.from_crs(epsg,"EPSG:4326",always_xy=True); geod=Geod(ellps="WGS84")
l0=[t for t in m['tiles'] if t['level']==0]
def load(t): return np.fromfile(f"{out}/{t['height']}",dtype='<f4').reshape(S,S)
# 1. tile bounds_wgs84 vs independent transform of the four corners
worst=0
for t in l0:
    b=t['bounds_utm']; pts=[(b[0],b[1]),(b[2],b[1]),(b[0],b[3]),(b[2],b[3])]
    ll=[tf.transform(*p) for p in pts]
    w,e=min(p[0] for p in ll),max(p[0] for p in ll); s,n=min(p[1] for p in ll),max(p[1] for p in ll)
    bw=t['bounds_wgs84']; worst=max(worst,abs(bw[0]-w),abs(bw[1]-s),abs(bw[2]-e),abs(bw[3]-n))
print(f"1. tile bounds_wgs84 vs pyproj corners: worst diff {worst*111320:.4f} m ({len(l0)} tiles)")
# 2. adjacent-sample ground distance vs grid spacing / point scale (geod)
def vertex(t,r,c): # sample row r (north->south), col c -> E,N
    b=t['bounds_utm']; return b[0]+c*cell, b[3]-r*cell
rng=random.Random(1); errs=[]
for _ in range(400):
    t=rng.choice(l0); r,c=rng.randrange(S-1),rng.randrange(S-1)
    e,n=vertex(t,r,c); a=tf.transform(e,n); b2=tf.transform(e+cell,n); c2=tf.transform(e,n-cell)
    dE=geod.inv(a[0],a[1],b2[0],b2[1])[2]; dN=geod.inv(a[0],a[1],c2[0],c2[1])[2]
    k=Transformer.from_crs("EPSG:4326",epsg,always_xy=True).transform  # scale from grid:
    errs.append((dE/cell,dN/cell))
errs=np.array(errs); print(f"2. ground metres per grid metre (E dir) {errs[:,0].min():.5f}..{errs[:,0].max():.5f}; (N dir) {errs[:,1].min():.5f}..{errs[:,1].max():.5f}  (manifest scale {m['stats'].get('utm_scale_min')}..{m['stats'].get('utm_scale_max')}: ground = 1/scale)")
# 3. elevation vs raw source: tile value at vertex vs raw GMRT bilinear at the independently computed lon/lat; compare with shifted positions
rawf=sorted(glob.glob(raw+"/*/topo.tif"),key=lambda p:-__import__('os').path.getsize(p))[0]
pts=[];vals=[]
for _ in range(600):
    t=rng.choice(l0); H=load(t); r,c=rng.randrange(1,S-1),rng.randrange(1,S-1)
    if H[r,c]<-9000: continue
    e,n=vertex(t,r,c); pts.append((e,n)); vals.append(H[r,c])
vals=np.array(vals)
def raw_at(dx,dy):
    ll=[tf.transform(e+dx,n+dy) for e,n in pts]
    inp="\n".join(f"{x:.9f} {y:.9f}" for x,y in ll)
    o=subprocess.run(["gdallocationinfo","-valonly","-wgs84","-r","bilinear",rawf],input=inp+"\n",capture_output=True,text=True).stdout.split()
    return np.array([float(v) for v in o])
res={}
for dx,dy in [(0,0),(cell,0),(-cell,0),(0,cell),(0,-cell)]:
    d=raw_at(dx,dy)-vals; res[(dx,dy)]=d
    print(f"3. offset ({dx:+4d},{dy:+4d}) m: rms {np.sqrt(np.mean(d**2)):7.2f} m  median|d| {np.median(np.abs(d)):6.2f}")
d0=res[(0,0)]; print(f"   raw={rawf.split('/')[-1]} n={len(vals)}; at zero offset: p95|d| {np.percentile(np.abs(d0),95):.2f} m, max|d| {np.abs(d0).max():.2f} m")
