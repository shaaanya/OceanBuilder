# pylint: skip-file
# One-shot measurement script; see README.md. Not production code.
import json,glob,math,numpy as np
from pyproj import CRS,Transformer
R='AloneV2/Assets/Data/WorldData/Packs/'
print('pack  tiles  dem_paths  hasShoreDepth  sources  terrain_y_minmax(sample)  clusters(>=1 tile gap 3)  maxClusterKm  zonesPerCluster(max)')
for d in sorted(glob.glob(R+'act?_*/')):
    if d.endswith('nemo/'): continue
    m=json.load(open(d+'manifest.json')); lid=m['legId']
    T=[(t['x'],t['z']) if 'x' in t else tuple(t) for t in m['tiles']] if isinstance(m['tiles'][0],dict) else [tuple(t) for t in m['tiles']]
    ys=[]
    for f in sorted(glob.glob(d+'tile_*_terrain.json'))[:25]:
        v=np.array(json.load(open(f))['vertices']); ys.append((v[:,1].min(),v[:,1].max()))
    yr=(min(a for a,b in ys),max(b for a,b in ys)) if ys else None
    # cluster by 8-neighbour within 3 tiles
    T=set(T); seen=set(); cl=[]
    for t in T:
        if t in seen: continue
        st=[t]; seen.add(t); c=[t]
        while st:
            a=st.pop()
            for dx in range(-3,4):
                for dz in range(-3,4):
                    b=(a[0]+dx,a[1]+dz)
                    if b in T and b not in seen: seen.add(b); st.append(b); c.append(b)
        cl.append(c)
    aeqd=CRS.from_proj4(f"+proj=aeqd +lat_0={m['originLat']} +lon_0={m['originLon']} +datum=WGS84 +units=m")
    inv=Transformer.from_crs(aeqd,"EPSG:4326",always_xy=True)
    mx=0;mz=0
    for c in cl:
        xs=np.array([a[0] for a in c])*10+5; zs=np.array([a[1] for a in c])*10+5
        ext=max(np.ptp(xs),np.ptp(zs))+10; mx=max(mx,ext)
        lo,la=inv.transform(xs*1000,zs*1000); z=set(((lo+180)//6+1).astype(int)); mz=max(mz,len(z))
    print(f"{lid[:24]:24s} {len(T):4d} {len(m.get('fallbackDemPaths',[]))+len(m.get('nearDemPaths',[]))+len(m.get('midDemPaths',[]))+len(m.get('farDemPaths',[]))} {m.get('hasShoreDepthTextures')} {len(m.get('sources',[]))} {yr} clusters={len(cl)} maxext={mx:.0f}km maxzones={mz}")
