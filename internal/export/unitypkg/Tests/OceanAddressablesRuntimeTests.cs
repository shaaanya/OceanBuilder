// Play-mode test: loads the world index and tiles through Addressables (not the AssetDatabase), links
// neighbours with the runtime helper and checks manifest checkpoints against Terrain.SampleHeight.
// Run after `Ocean > Addressables > Build And Check` with the play mode script set to "Use Existing Build".
using System.Collections;
using System.Collections.Generic;
using NUnit.Framework;
using UnityEngine;
using UnityEngine.AddressableAssets;
using UnityEngine.ResourceManagement.ResourceLocations;
using UnityEngine.TestTools;

namespace OceanBuilder.World.Tests
{
    public class OceanAddressablesRuntimeTests
    {
        [UnityTest]
        public IEnumerator TilesLoadThroughAddressablesAndMatchCheckpoints()
        {
            var locs = Addressables.LoadResourceLocationsAsync("ocean-index");
            yield return locs;
            Assert.That(locs.Result.Count, Is.GreaterThan(0), "no Addressable carries the label ocean-index; import a world first");
            foreach (IResourceLocation loc in locs.Result)
            {
                var h = Addressables.LoadAssetAsync<OceanWorldIndex>(loc);
                yield return h;
                var index = h.Result;
                Assert.IsNotNull(index, loc.PrimaryKey);
                Assert.That(index.checkpoints.Length, Is.GreaterThan(0));
                foreach (var s in index.seamarks) Assert.IsNotNull(index.Get(s.tileId), "seamark " + s.id + " has an unknown tile");
                Debug.Log("OCEAN_SEAMARKS " + index.seamarks.Length);

                // The point of this test is real bundle loading, not AssetDatabase shortcuts.
                var tileLocs = Addressables.LoadResourceLocationsAsync(index.tiles[0].address);
                yield return tileLocs;
                StringAssert.Contains("BundledAssetProvider", tileLocs.Result[0].ProviderId, "play mode is not using built bundles: run Ocean > Addressables > Build And Check first");

                var loaded = new Dictionary<string, Terrain>();
                var instances = new List<GameObject>();
                foreach (var cp in index.checkpoints)
                {
                    var tile = index.TileAt(index.UtmToUnity(cp.easting, cp.northing));
                    Assert.IsNotNull(tile, cp.name + " is outside every tile");
                    if (loaded.ContainsKey(tile.id)) continue;
                    var inst = Addressables.InstantiateAsync(tile.address);
                    yield return inst;
                    Assert.IsNotNull(inst.Result, tile.address);
                    instances.Add(inst.Result);
                    loaded[tile.id] = inst.Result.GetComponent<Terrain>();
                }
                foreach (var kv in loaded) index.LinkNeighbours(index.Get(kv.Key), kv.Value, id => loaded.TryGetValue(id, out var t) ? t : null);
                foreach (var cp in index.checkpoints)
                {
                    var p = index.UtmToUnity(cp.easting, cp.northing);
                    var terrain = loaded[index.TileAt(p).id];
                    float y = terrain.SampleHeight(p) + terrain.transform.position.y;
                    Assert.AreEqual(cp.elevationM, y, 0.05f, cp.name);
                }
                // Neighbour links: any two loaded tiles that touch must reference each other.
                foreach (var kv in loaded)
                {
                    var e = index.Get(kv.Key);
                    if (!string.IsNullOrEmpty(e.east) && loaded.TryGetValue(e.east, out var east))
                    {
                        Assert.AreSame(east, kv.Value.rightNeighbor, kv.Key + " east");
                        Assert.AreSame(kv.Value, east.leftNeighbor, e.east + " west");
                    }
                }

                // Render the loaded tiles from beyond Terrain.basemapDistance (the far-LOD terrain path) and look for the
                // pink error shader: this is what a missing or incompatible render pipeline setup looks like.
                var focus = index.Get(new List<string>(loaded.Keys)[0]);
                var centre = focus.position + new Vector3(focus.size.x / 2, 0, focus.size.z / 2);
                var cam = new GameObject("OceanProbeCamera").AddComponent<Camera>();
                cam.clearFlags = CameraClearFlags.SolidColor; cam.backgroundColor = Color.black; cam.farClipPlane = 30000;
                float off = index.tileSpanM * 0.57f; // beyond basemapDistance for any tile size
                cam.transform.position = centre + new Vector3(0, off, -off); cam.transform.LookAt(centre);
                yield return null; yield return null;
                var rt = new RenderTexture(256, 256, 24); cam.targetTexture = rt; cam.Render(); cam.Render();
                RenderTexture.active = rt;
                var img = new Texture2D(256, 256, TextureFormat.RGB24, false); img.ReadPixels(new Rect(0, 0, 256, 256), 0, 0); img.Apply();
                RenderTexture.active = null;
                int pink = 0; foreach (var c in img.GetPixels()) if (c.r > 0.8f && c.g < 0.3f && c.b > 0.8f) pink++;
                float frac = pink / (256f * 256f);
                // An all-background frame also has no pink: require that terrain actually fills part of the picture.
                int seen = 0; foreach (var c in img.GetPixels()) if (Mathf.Abs(c.r - cam.backgroundColor.r) + Mathf.Abs(c.g - cam.backgroundColor.g) + Mathf.Abs(c.b - cam.backgroundColor.b) > 0.05f) seen++;
                float cover = seen / (256f * 256f);
                Debug.Log("OCEAN_TERRAIN_COVERAGE " + cover.ToString("F4"));
                Debug.Log("OCEAN_PINK_FRACTION " + frac.ToString("F4") + " pipeline=" + (UnityEngine.Rendering.GraphicsSettings.currentRenderPipeline != null ? UnityEngine.Rendering.GraphicsSettings.currentRenderPipeline.GetType().Name : "Built-in"));
                Object.Destroy(cam.gameObject);
                Assert.Greater(cover, 0.2f, "probe camera sees no terrain");
                Assert.Less(frac, 0.01f, "terrain renders with the pink error shader from a distance");
                foreach (var g in instances) Addressables.ReleaseInstance(g);
                Debug.Log("OCEAN_RUNTIME_OK world=" + index.worldName + " tilesLoaded=" + loaded.Count + " checkpoints=" + index.checkpoints.Length);
            }
        }
    }
}
