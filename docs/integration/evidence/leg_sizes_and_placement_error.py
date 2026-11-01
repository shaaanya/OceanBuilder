# pylint: skip-file
# One-shot measurement script; see README.md. Not production code.
import json,glob,math,numpy as np
from pyproj import Geod,Transformer,CRS
g=Geod(ellps='WGS84')
print('ACT   len_km  zones(with >=2%% of samples)  bbox_w_km bbox_h_km  corridor80km_area_km2  samples@48m(M)  pack_tiles  tile_area_km2  OBtiles@6.1km')
tot_tiles=0
for f in sorted(glob.glob('tools/world-pipeline/routes/act?_*.json')):
    if 'nemo' in f or 'test' in f: continue
    r=json.load(open(f)); w=r['waypoints']
    pts=[];L=0
    for (a,b),(c,d) in zip(w,w[1:]):
        dist=g.inv(b,a,d,c)[2];L+=dist
        pts+=g.npts(b,a,d,c,max(2,int(dist/5000)))
    pts=np.array(pts); zones=((pts[:,0]+180)//6+1).astype(int)
    u,cnt=np.unique(zones,return_counts=True); zs=[f'{z}({100*c/len(zones):.0f}%)' for z,c in zip(u,cnt) if c/len(zones)>0.02]
    lat0=pts[:,1].mean(); kmlon=111.32*math.cos(math.radians(lat0))
    dlon=np.ptp(pts[:,0]);
    if dlon>180: dlon=360-dlon
    bw=dlon*kmlon; bh=np.ptp(pts[:,1])*111.0
    area=L/1000*80
    m=json.load(open(f"AloneV2/Assets/Data/WorldData/Packs/{r['legId']}/manifest.json"))
    nt=len(m['tiles']); tot_tiles+=nt
    print(f"{r['legId'][:24]:24s} {L/1000:6.0f} {','.join(zs):26s} {bw:7.0f} {bh:7.0f} {area:10.0f} {area*1e6/48/48/1e6:8.2f} {nt:5d} {nt*100:8d} {nt*100/(6.144**2):7.0f}")
print('total pack tiles',tot_tiles)
# placement disagreement at Mindelo between AEQD(Falmouth) rigid-block and true local geometry
o=(50.14,-5.04); B=(16.89,-24.99)
aeqd=CRS.from_proj4(f"+proj=aeqd +lat_0={o[0]} +lon_0={o[1]} +datum=WGS84 +units=m")
T=Transformer.from_crs("EPSG:4326",aeqd,always_xy=True)
az1,baz,_=g.inv(o[1],o[0],B[1],B[0]); az2=baz-180
th=math.radians(az2-az1)
bx,by=T.transform(B[1],B[0])
for name,az in (('east',90),('north',0),('towards origin',None)):
    for dkm in (1,5,10):
        a = az if az is not None else (g.inv(B[1],B[0],o[1],o[0])[0])
        lo,la,_=g.fwd(B[1],B[0],a,dkm*1000)
        px,py=T.transform(lo,la); dx,dy=px-bx,py-by
        c,s=math.cos(-th),math.sin(-th)  # rotate grid vector by -theta (clockwise az2-az1)
        rx,ry=dx*c-dy*s, dx*s+dy*c
        tx,ty=dkm*1000*math.sin(math.radians(a)),dkm*1000*math.cos(math.radians(a))
        print(f'Mindelo {name:14s} {dkm:2d} km: game-tile placement error {math.hypot(rx-tx,ry-ty):6.0f} m  (sign-flip variant {math.hypot(dx*math.cos(th)-dy*math.sin(th)-tx, dx*math.sin(th)+dy*math.cos(th)-ty):.0f} m)')
