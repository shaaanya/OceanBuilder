// Player smoke phase for Addressables: every imported tile of every level (index.tiles + index.lodTiles) is loaded by
// its address from the content built into the player, checked against the index, and released. Also: an address that
// does not exist resolves to no location (no exception), and releasing the handles lets the terrains go.
// OceanStandaloneBuild adds this component when Addressables is installed and builds the content before the player.
using System.Collections;
using System.Collections.Generic;
using UnityEngine;
using UnityEngine.AddressableAssets;
using UnityEngine.ResourceManagement.AsyncOperations;

namespace OceanCompiler.World
{
    public class OceanPlayerSmokeAddressables : MonoBehaviour, IOceanSmokePhase
    {
        public IEnumerator Run(OceanWorldIndex index, OceanSmokeReport report)
        {
            var init = Addressables.InitializeAsync(false); // false: keep the handle valid after completion so its status can be read
            yield return init;
            report.Check(init.IsValid() && init.Status == AsyncOperationStatus.Succeeded, "Addressables did not initialise");
            if (init.IsValid()) Addressables.Release(init);
            int ok = 0, failed = 0;
            int before = FindObjectsByType<Terrain>(FindObjectsSortMode.None).Length;
            var all = new List<OceanTileEntry>(index.lodTiles);
            // two level-0 tiles as well: the streamer path
            for (int i = 0; i < index.tiles.Length && i < 2; i++) all.Add(index.tiles[i]);
            var handles = new List<AsyncOperationHandle<GameObject>>();
            var spawned = new List<GameObject>();
            foreach (var e in all)
            {
                var h = Addressables.LoadAssetAsync<GameObject>(e.address);
                yield return h;
                if (h.Status != AsyncOperationStatus.Succeeded || h.Result == null) { failed++; report.Fail("address " + e.address + " did not load"); continue; }
                handles.Add(h);
                var go = Instantiate(h.Result);
                var terrain = go.GetComponent<Terrain>();
                if (terrain == null) { failed++; report.Fail(e.address + " has no Terrain"); Destroy(go); continue; }
                spawned.Add(go);
                var pos = terrain.transform.position;
                bool good = (pos - e.position).magnitude < 0.01f && Mathf.Abs(terrain.terrainData.size.x - e.size.x) < 0.01f;
                if (good) ok++; else { failed++; report.Fail(e.address + " position/size differ from the index"); }
            }
            // an address that is not in the catalog: no locations, nothing thrown
            var loc = Addressables.LoadResourceLocationsAsync("ocean/" + index.worldName + "/L99_0_0");
            yield return loc;
            int missingLocations = loc.Status == AsyncOperationStatus.Succeeded ? loc.Result.Count : -1;
            report.Check(missingLocations == 0, "a missing address must resolve to zero locations");
            Addressables.Release(loc);
            int during = FindObjectsByType<Terrain>(FindObjectsSortMode.None).Length;
            report.Check(during == before + spawned.Count, "terrains while loaded " + during + ", expected " + (before + spawned.Count));
            foreach (var go in spawned) Destroy(go);
            foreach (var h in handles) Addressables.Release(h);
            yield return null;
            yield return Resources.UnloadUnusedAssets();
            int after = FindObjectsByType<Terrain>(FindObjectsSortMode.None).Length;
            report.Check(after == before, "terrains after release " + after + ", expected " + before);
            Debug.Log("OCEAN_PLAYER_ADDRESSABLES world=" + index.worldName + " loaded_ok=" + ok + " failed=" + failed + " missing_address_locations=" + missingLocations + " terrains_before=" + before + " during=" + during + " after=" + after);
        }
    }
}
