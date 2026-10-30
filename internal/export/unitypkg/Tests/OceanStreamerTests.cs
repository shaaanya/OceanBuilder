using System.Collections;
using System.Collections.Generic;
using NUnit.Framework;
using UnityEngine;
using UnityEngine.AddressableAssets;
using UnityEngine.ResourceManagement.AsyncOperations;
using UnityEngine.TestTools;

namespace OceanCompiler.World.Tests
{
    public class OceanStreamerTests
    {
        // 3x3 tiles of 1000 m; row 0 is the NORTH row (manifest convention), Unity z grows north.
        static OceanWorldIndex Grid3()
        {
            var ix = ScriptableObject.CreateInstance<OceanWorldIndex>();
            ix.tilesX = ix.tilesY = 3; ix.tileSpanM = 1000;
            var list = new List<OceanTileEntry>();
            for (int r = 0; r < 3; r++)
                for (int c = 0; c < 3; c++)
                    list.Add(new OceanTileEntry { id = "L0_" + c + "_" + (2 - r), col = c, row = r, position = new Vector3(c * 1000, 0, (2 - r) * 1000), size = new Vector3(1000, 10, 1000) });
            ix.tiles = list.ToArray();
            return ix;
        }

        [Test]
        public void PlanLoadsNearestFirstAndUnloadsWithHysteresis()
        {
            var ix = Grid3();
            var load = new List<OceanTileEntry>(); var unload = new List<string>();
            var centre = new Vector3(1500, 0, 1500);
            OceanStreamingPlan.Compute(ix, centre, 100, 100, new List<string>(), load, unload);
            Assert.AreEqual(1, load.Count, "inside one tile and 100 m from no other: only that tile");
            Assert.AreEqual("L0_1_1", load[0].id);
            OceanStreamingPlan.Compute(ix, centre, 600, 600, new List<string>(), load, unload);
            Assert.AreEqual(5, load.Count, "edge neighbours are 500 m away, diagonals 707 m");
            Assert.AreEqual("L0_1_1", load[0].id, "nearest first");

            // A loaded tile 550 m away stays while within the unload radius, goes beyond it; load radius never exceeds unload.
            var have = new List<string> { "L0_0_1" };
            OceanStreamingPlan.Compute(ix, new Vector3(1550, 0, 1500), 100, 300, have, load, unload);
            CollectionAssert.Contains(unload, "L0_0_1", "tile edge is 550 m away, beyond the 300 m unload radius");
        }

        [Test]
        public void PlanKeepsTilesInsideUnloadRadius()
        {
            var ix = Grid3();
            var load = new List<OceanTileEntry>(); var unload = new List<string>();
            var have = new List<string> { "L0_0_1" };
            // Point at x=1100: tile L0_0_1 spans x 0..1000, so it is 100 m away.
            OceanStreamingPlan.Compute(ix, new Vector3(1100, 0, 1500), 50, 300, have, load, unload);
            CollectionAssert.IsEmpty(unload);
            OceanStreamingPlan.Compute(ix, new Vector3(1100, 0, 1500), 50, 80, have, load, unload);
            CollectionAssert.Contains(unload, "L0_0_1");
        }

        IEnumerator Until(System.Func<bool> cond, string what)
        {
            for (int i = 0; i < 600 && !cond(); i++) yield return null;
            Assert.IsTrue(cond(), "timed out waiting for " + what);
        }

        [UnityTest]
        public IEnumerator StreamerLoadsLinksAndUnloadsAroundMovingTarget()
        {
            var locs = Addressables.LoadResourceLocationsAsync("ocean-index");
            yield return locs;
            Assert.That(locs.Result.Count, Is.GreaterThan(0), "build Addressables first (Ocean > Addressables > Build And Check)");
            var h = Addressables.LoadAssetAsync<OceanWorldIndex>(locs.Result[0]);
            yield return h;
            var index = h.Result;
            float span = index.tileSpanM;
            Assert.That(index.tilesX, Is.GreaterThanOrEqualTo(3));
            Assert.That(index.tilesY, Is.GreaterThanOrEqualTo(3));

            var target = new GameObject("StreamTarget").transform;
            var go = new GameObject("Streamer");
            var s = go.AddComponent<OceanTileStreamer>();
            s.index = index; s.target = target; s.maxInFlight = 4;
            s.loadRadiusM = span * 0.6f; s.unloadRadiusM = span * 0.7f; // own tile, 4 edge neighbours; diagonals are 0.707 span away

            // Middle of the world: tile (col 1, row 1) has all four neighbours.
            var mid = index.tiles[0];
            foreach (var t in index.tiles) if (t.col == 1 && t.row == 1) mid = t;
            target.position = mid.position + new Vector3(span / 2, 0, span / 2);
            yield return Until(() => s.LoadedCount == 5 && s.PendingCount == 0, "centre tile and 4 neighbours");
            var tc = s.GetTerrain(mid.id);
            Assert.IsNotNull(tc);
            Assert.IsNotNull(tc.leftNeighbor); Assert.IsNotNull(tc.rightNeighbor); Assert.IsNotNull(tc.topNeighbor); Assert.IsNotNull(tc.bottomNeighbor);

            // Sail to the SE corner tile: the centre tiles must go and the corner arrive.
            var corner = index.tiles[0];
            foreach (var t in index.tiles) if (t.col == index.tilesX - 1 && t.row == index.tilesY - 1) corner = t;
            target.position = corner.position + new Vector3(span / 2, 0, span / 2);
            yield return Until(() => s.IsLoaded(corner.id) && !s.IsLoaded(mid.id) && s.PendingCount == 0, "corner loaded, centre unloaded");
            Assert.AreEqual(3, s.LoadedCount, "corner tile plus its two edge neighbours");
            Assert.AreEqual(3, Object.FindObjectsByType<Terrain>(FindObjectsSortMode.None).Length, "unloaded terrains must be destroyed");

            Object.Destroy(go); Object.Destroy(target.gameObject);
            yield return null;
            Assert.AreEqual(0, Object.FindObjectsByType<Terrain>(FindObjectsSortMode.None).Length, "destroying the streamer releases every tile");
            Debug.Log("OCEAN_STREAMER_OK");
        }
    }
}
