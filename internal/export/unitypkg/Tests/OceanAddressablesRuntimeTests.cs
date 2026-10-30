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

namespace OceanCompiler.World.Tests
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
                foreach (var g in instances) Addressables.ReleaseInstance(g);
                Debug.Log("OCEAN_RUNTIME_OK world=" + index.worldName + " tilesLoaded=" + loaded.Count + " checkpoints=" + index.checkpoints.Length);
            }
        }
    }
}
