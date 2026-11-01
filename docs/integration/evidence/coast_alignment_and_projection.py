# pylint: skip-file
# One-shot measurement script; see README.md. Not production code.
import json, glob, math, re, numpy as np
from pyproj import Transformer, CRS
P='/Users/shaaaanya/Documents/projects/Unity/Alone/AloneV2/Assets/Data/WorldData/Packs/act1_falmouth_mindelo/'
aeqd=CRS.from_proj4("+proj=aeqd +lat_0=50.14 +lon_0=-5.04 +datum=WGS84 +units=m")
a2w=Transformer.from_crs(aeqd,"EPSG:4326",always_xy=True)
w2u=Transformer.from_crs("EPSG:4326","EPSG:32630",always_xy=True)
# --- Alone pack coastline vertices near Falmouth bbox, and elevation stats
pts=[];zmax=-1e9;zmin=1e9
for f in glob.glob(P+'tile_*.geojson'):
    m=re.search(r'tile_(-?\d+)_(-?\d+)\.geojson',f); tx,tz=int(m[1]),int(m[2])
    if abs(tx)>3 or abs(tz)>3: continue
    g=json.load(open(f))['features'][0]['geometry']
    polys=g['coordinates'] if g['type']=='MultiPolygon' else [g['coordinates']]
    for poly in polys:
        for ring in poly:
            for e,n,z in ring:
                zmax=max(zmax,z);zmin=min(zmin,z)
                pts.append((tx*10000+e,tz*10000+n))
pts=np.array(pts); lon,lat=a2w.transform(pts[:,0],pts[:,1]); E,N=w2u.transform(lon,lat)
print('alone ring verts near Falmouth',len(pts),'z range',zmin,zmax)
# --- OB OSM coastline (WGS84 lines) densified to UTM
c=json.load(open('output/falmouth/coastline.wgs84.geojson'))
segs=[]
def lines(g):
    t=g['type']
    if t=='LineString': yield g['coordinates']
    elif t=='MultiLineString':
        for l in g['coordinates']: yield l
    elif t=='GeometryCollection':
        for gg in g['geometries']: yield from lines(gg)
for f in c['features']:
    for l in lines(f['geometry']): segs.append(np.array(l))
dense=[]
for l in segs:
    x,y=w2u.transform(l[:,0],l[:,1])
    for i in range(len(x)-1):
        n=max(1,int(math.hypot(x[i+1]-x[i],y[i+1]-y[i])//5))
        dense.append(np.stack([np.linspace(x[i],x[i+1],n,endpoint=False),np.linspace(y[i],y[i+1],n,endpoint=False)],1))
D=np.concatenate(dense); print('OSM dense pts',len(D))
from scipy.spatial import cKDTree
t=cKDTree(D)
m=json.load(open('output/falmouth/world.manifest.json'))['grid']
x0=m['origin_easting']; x1=x0+4*6144; y1=m['origin_northing_top']; y0=y1-4*6144
inb=(E>x0+500)&(E<x1-500)&(N>y0+500)&(N<y1-500)
d,_=t.query(np.stack([E[inb],N[inb]],1))
# only count verts that are real coastline (not tile-edge cuts): exclude those with local coord near multiple of 10000
loc=pts[inb]%10000; edge=(np.minimum(loc,10000-loc)<2).any(1)
d=d[~edge]
print('alone GSHHG ring verts in OB bbox (non-edge):',len(d),'dist to OSM coast: median %.0f p90 %.0f p99 %.0f max %.0f m'%tuple(np.percentile(d,[50,90,99,100])))
# --- projection differences: AEQD(Falmouth origin) vs UTM at test sites
for name,(la,lo) in {'Falmouth':(50.14,-5.04),'Finisterre':(42.88,-9.27),'Lisbon':(38.7,-9.14),'Tenerife':(28.3,-16.4),'Mindelo':(16.89,-24.99)}.items():
    zone=int((lo+180)//6)+1; utm=Transformer.from_crs("EPSG:4326",f"EPSG:{32600+zone}",always_xy=True)
    ae=Transformer.from_crs("EPSG:4326",aeqd,always_xy=True)
    # a 6144 m square in UTM at this place: measure its AEQD edge length/rotation
    ux,uy=utm.transform(lo,la); inv=Transformer.from_crs(f"EPSG:{32600+zone}","EPSG:4326",always_xy=True)
    def A(dx,dy):
        l,b=inv.transform(ux+dx,uy+dy); return np.array(ae.transform(l,b))
    p0,pe,pn=A(0,0),A(6144,0),A(0,6144)
    le=np.linalg.norm(pe-p0)/6144; ang=math.degrees(math.atan2(pe[1]-p0[1],pe[0]-p0[0]))
    print(f'{name:11s} UTM zone {zone}N: AEQD(Falmouth) scale of UTM 6.1 km tile {le:.4f}, grid rotation {ang:+.2f} deg; UTM k={0 if False else ""}')
print('--- refined')
d_all,_=t.query(np.stack([E[inb],N[inb]],1))
for r in (500,1000,2000):
    s=d_all[d_all<r]; print(f'verts within {r} m of an OSM coast sample: {len(s)}/{len(d_all)}  median {np.median(s):.0f} p90 {np.percentile(s,90):.0f} p99 {np.percentile(s,99):.0f}')
# reverse: OSM coast samples inside bbox -> nearest Alone vertex (Alone rings are 10 m densified so ok)
at=cKDTree(np.stack([E,N],1))
inD=(D[:,0]>x0+500)&(D[:,0]<x1-500)&(D[:,1]>y0+500)&(D[:,1]<y1-500)
dd,_=at.query(D[inD]); 
print('OSM samples in bbox with an Alone coast vertex within 300 m: %.1f%%; median %.0f m p90 %.0f m'%(100*(dd<300).mean(),np.median(dd),np.percentile(dd,90)))
