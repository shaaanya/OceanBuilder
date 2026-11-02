// How OceanLodStreamer gets a tile into the scene. The Addressables implementation lives in the Streaming assembly
// (OceanAddressablesTileLoader, registered as the default when com.unity.addressables is installed); OceanPrefabTileLoader
// here serves tests, small worlds and projects that do not use Addressables.
using System;
using System.Collections.Generic;
using UnityEngine;

namespace OceanCompiler.World
{
    /// <summary>One request to load a tile. Filled in by the loader.</summary>
    public sealed class OceanTileLoad
    {
        public OceanTileEntry entry;
        public bool IsDone, Failed;
        public string Error;
        public GameObject Instance;
        public object Token;          // loader-private (an Addressables handle, a frame counter...)
        public bool ReleaseRequested; // Release was called before the load finished: free everything on completion and do not call onDone
    }

    public interface IOceanTileLoader
    {
        /// <summary>Starts loading the tile's prefab as a child of <paramref name="parent"/>. <paramref name="onDone"/> is called once, on the main thread, when it finished or failed (never before this method returns).</summary>
        OceanTileLoad Begin(OceanTileEntry entry, Transform parent, Action<OceanTileLoad> onDone);
        /// <summary>Destroys the instance and drops every reference the load holds. Legal at any time; a load still in flight is freed when it completes and then does not call onDone.</summary>
        void Release(OceanTileLoad load);
        /// <summary>Called once per frame by the streamer (loaders that need a pump use it).</summary>
        void Tick();
    }

    /// <summary>Supplies the loader a streamer uses when none is assigned. The Streaming assembly sets it when Addressables is present.</summary>
    public static class OceanTileLoaders
    {
        public static Func<IOceanTileLoader> DefaultFactory;
    }

    /// <summary>Loads tiles from prefab references by id, after a configurable number of frames. Failures can be injected per id.</summary>
    public sealed class OceanPrefabTileLoader : IOceanTileLoader
    {
        sealed class Pending { public OceanTileLoad load; public Action<OceanTileLoad> onDone; public Transform parent; public int framesLeft; }

        readonly Dictionary<string, GameObject> prefabs = new Dictionary<string, GameObject>();
        readonly List<Pending> pending = new List<Pending>();
        /// <summary>Frames between Begin and completion.</summary>
        public int latencyFrames = 2;
        /// <summary>Ids that fail to load (as a missing or corrupt asset would).</summary>
        public readonly HashSet<string> failIds = new HashSet<string>();
        /// <summary>Counters for tests: loads started, instances alive, loads that completed after release was requested, most loads in flight at once.</summary>
        public int begun, live, releasedInFlight, maxConcurrent;
        public readonly Dictionary<string, int> beginsById = new Dictionary<string, int>();

        public OceanPrefabTileLoader(IEnumerable<GameObject> prefabs) { foreach (var p in prefabs) if (p != null) this.prefabs[p.name] = p; }

        public OceanTileLoad Begin(OceanTileEntry entry, Transform parent, Action<OceanTileLoad> onDone)
        {
            var load = new OceanTileLoad { entry = entry };
            begun++;
            beginsById[entry.id] = beginsById.TryGetValue(entry.id, out var n) ? n + 1 : 1;
            pending.Add(new Pending { load = load, onDone = onDone, parent = parent, framesLeft = Math.Max(1, latencyFrames) });
            maxConcurrent = Math.Max(maxConcurrent, pending.Count);
            return load;
        }

        public void Tick()
        {
            for (int i = 0; i < pending.Count;)
            {
                var p = pending[i];
                if (--p.framesLeft > 0) { i++; continue; }
                pending.RemoveAt(i);
                var load = p.load;
                bool injected = failIds.Contains(load.entry.id);
                if (injected || !prefabs.TryGetValue(load.entry.id, out var prefab) || prefab == null)
                { load.Failed = true; load.Error = "no prefab for " + load.entry.id + (injected ? " (injected failure)" : ""); }
                else { load.Instance = UnityEngine.Object.Instantiate(prefab, p.parent); load.Instance.name = load.entry.id; live++; }
                load.IsDone = true;
                if (load.ReleaseRequested) { releasedInFlight++; Free(load); continue; }
                p.onDone(load);
            }
        }

        public void Release(OceanTileLoad load)
        {
            if (load == null) return;
            if (!load.IsDone) { load.ReleaseRequested = true; return; }
            Free(load);
        }

        void Free(OceanTileLoad load)
        {
            if (load.Instance == null) return;
            live--; UnityEngine.Object.Destroy(load.Instance); load.Instance = null;
        }
    }
}
