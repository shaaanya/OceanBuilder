# Independent check of a built world across 180 against the raw GMRT pieces in the cache. Usage: verify_antimeridian.py <world dir> <cache dir>
import json,sys,subprocess,random,glob,numpy as np
from pyproj import Transformer
out,cache=sys.argv[1],sys.argv[2]
m=json.load(open(out+"/world.manifest.json")); g=m['grid']; S=g['samples_per_tile']; cell=g['cell_m']
tf=Transformer.from_crs(f"EPSG:{m['crs']['epsg']}","EPSG:4326",always_xy=True)
files=[]
for f in glob.glob(cache+"/gmrt/*/topo.tif"):
    i=json.loads(subprocess.run(["gdalinfo","-json",f],capture_output=True,text=True).stdout); gt=i['geoTransform']; W,H=i['size']
    files.append((f,gt[0],gt[0]+gt[1]*W,gt[3]+gt[5]*H,gt[3])); print("raw",f.split('/')[-2],"lon %.5f..%.5f lat %.4f..%.4f px %dx%d res %.6f"%(gt[0],gt[0]+gt[1]*W,gt[3]+gt[5]*H,gt[3],W,H,gt[1]))
l0=[t for t in m['tiles'] if t['level']==0]
def load(t): return np.fromfile(f"{out}/{t['height']}",dtype='<f4').reshape(S,S)
rng=random.Random(2); P=[]
for t in l0:
    H=load(t); b=t['bounds_utm']
    for _ in range(40):
        r,c=rng.randrange(1,S-1),rng.randrange(1,S-1)
        if H[r,c]>-9000:
            lon,lat=tf.transform(b[0]+c*cell,b[3]-r*cell); P.append((lon,lat,float(H[r,c])))
def raw(lon,lat):
    for f,w,e,s,n in files:
      for L in (lon,lon+360):
        if w<=L<=e and s<=lat<=n:
            return float(subprocess.run(["gdallocationinfo","-valonly","-wgs84","-r","bilinear",f],input=f"{L} {lat}\n",capture_output=True,text=True).stdout.split()[0])
    return None
d=[];seam=[];none=0
for lon,lat,h in P:
    r=raw(lon,lat)
    if r is None: none+=1; continue
    d.append(r-h)
    if abs(abs(lon)-180)<0.03: seam.append(r-h)
d=np.array(d); print(f"points {len(P)}, outside raw files {none}; rms {np.sqrt(np.mean(d**2)):.2f} m median|d| {np.median(abs(d)):.2f} p95 {np.percentile(abs(d),95):.2f}")
print(f"seam (|lon|>179.97 or <-179.97) points: {len(seam)}; rms {np.sqrt(np.mean(np.array(seam)**2)):.2f}" if seam else "no seam points")
# transect across 180 at lat -16.85 via the world samples (ocean sample cmd) vs raw

