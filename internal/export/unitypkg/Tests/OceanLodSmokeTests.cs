using System.Collections;
using NUnit.Framework;
using UnityEngine;
using UnityEngine.AddressableAssets;
using UnityEngine.TestTools;

namespace OceanBuilder.World.Tests
{
    /// <summary>
    /// The runtime LOD scenarios (OceanLodSmoke) in the Editor's play mode, against the imported world's Addressables content
    /// (build it first: Ocean > Addressables > Build And Check; import with -oceanMaxLevel N so coarse levels exist).
    /// The same code runs in the built player (OceanStandaloneBuild), so Editor and player are checked alike.
    /// </summary>
    public class OceanLodSmokeTests
    {
        [UnityTest]
        public IEnumerator StreamerScenariosPass()
        {
            LogAssert.ignoreFailingMessages = true; // the corrupt-address scenario makes Addressables log an error on purpose
            var locs = Addressables.LoadResourceLocationsAsync("ocean-index");
            yield return locs;
            Assert.That(locs.Result.Count, Is.GreaterThan(0), "build Addressables first (Ocean > Addressables > Build And Check)");
            var h = Addressables.LoadAssetAsync<OceanWorldIndex>(locs.Result[0]);
            yield return h;
            var index = h.Result;
            Assert.That(index.maxLevelImported, Is.GreaterThanOrEqualTo(1), "import the world with -oceanMaxLevel 2 (or more) to test LOD levels");

            var report = new OceanSmokeReport();
            var go = new GameObject("OceanLodSmoke");
            var smoke = go.AddComponent<OceanLodSmoke>();
            yield return smoke.Run(index, report);
            Object.Destroy(go);
            Assert.AreEqual(0, report.failures, "LOD scenarios failed:" + report.detail);
        }

        [UnityTest]
        public IEnumerator SyntheticSeamHasNoCracksWithSkirts()
        {
            if (SystemInfo.graphicsDeviceType == UnityEngine.Rendering.GraphicsDeviceType.Null) Assert.Ignore("needs a graphics device");
            // the skirt material comes from the first imported tile that has one
            var locs = Addressables.LoadResourceLocationsAsync("ocean-index");
            yield return locs;
            var h = Addressables.LoadAssetAsync<OceanWorldIndex>(locs.Result[0]);
            yield return h;
            var index = h.Result;
            Material mat = null;
            foreach (var e in index.lodTiles.Length > 0 ? index.lodTiles : index.tiles)
            {
                var ph = Addressables.LoadAssetAsync<GameObject>(e.address);
                yield return ph;
                var sk = ph.Result != null ? ph.Result.GetComponent<OceanTileSkirts>() : null;
                if (sk != null && sk.east != null) { mat = sk.east.sharedMaterial; break; }
            }
            Assert.IsNotNull(mat, "no imported tile carries skirts: import with -oceanMaxLevel 1 or more");
            var report = new OceanSmokeReport();
            yield return OceanSeamProbe.RunSynthetic(report, mat, 0.5f);
            Assert.AreEqual(0, report.failures, "seam probe:" + report.detail);
        }
    }
}
