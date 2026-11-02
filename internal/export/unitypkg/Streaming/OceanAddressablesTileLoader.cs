// Loads tile prefabs by Addressables address (OceanTileEntry.address) for OceanLodStreamer. Registered as the default
// loader when this assembly is present, so a streamer with no explicit loader uses it.
using System;
using System.Collections.Generic;
using UnityEngine;
using UnityEngine.AddressableAssets;
using UnityEngine.ResourceManagement.AsyncOperations;

namespace OceanCompiler.World
{
    public sealed class OceanAddressablesTileLoader : IOceanTileLoader
    {
        [RuntimeInitializeOnLoadMethod(RuntimeInitializeLoadType.SubsystemRegistration)]
        static void Register() { OceanTileLoaders.DefaultFactory = () => new OceanAddressablesTileLoader(); }

        // onDone must not run before Begin returns (the caller registers the load first): a synchronous failure is reported from the next Tick.
        readonly List<(OceanTileLoad, Action<OceanTileLoad>)> deferred = new List<(OceanTileLoad, Action<OceanTileLoad>)>();

        public OceanTileLoad Begin(OceanTileEntry entry, Transform parent, Action<OceanTileLoad> onDone)
        {
            var load = new OceanTileLoad { entry = entry };
            AsyncOperationHandle<GameObject> handle;
            // An address missing from the catalog throws from InstantiateAsync in some versions and fails the handle in others: both are a failed load.
            try { handle = Addressables.InstantiateAsync(entry.address, parent, false); }
            catch (Exception e) { load.Failed = true; load.Error = e.Message; load.IsDone = true; deferred.Add((load, onDone)); return load; }
            load.Token = handle;
            handle.Completed += h =>
            {
                load.IsDone = true;
                if (h.Status != AsyncOperationStatus.Succeeded || h.Result == null)
                {
                    load.Failed = true;
                    load.Error = h.OperationException != null ? h.OperationException.Message : "load failed: " + entry.address;
                }
                else load.Instance = h.Result;
                if (load.ReleaseRequested) { ReleaseHandle(load); return; }
                onDone(load);
            };
            return load;
        }

        public void Tick()
        {
            if (deferred.Count == 0) return;
            var run = deferred.ToArray(); deferred.Clear();
            foreach (var (load, cb) in run) if (!load.ReleaseRequested) cb(load);
        }

        public void Release(OceanTileLoad load)
        {
            if (load == null) return;
            if (!load.IsDone) { load.ReleaseRequested = true; return; }
            ReleaseHandle(load);
        }

        static void ReleaseHandle(OceanTileLoad load)
        {
            if (load.Token is AsyncOperationHandle<GameObject> h && h.IsValid())
            {
                // ReleaseInstance destroys the instance and drops the reference the handle held; a failed handle has no instance and is released itself.
                if (load.Instance != null) Addressables.ReleaseInstance(load.Instance); else Addressables.Release(h);
            }
            else if (load.Instance != null) UnityEngine.Object.Destroy(load.Instance);
            load.Instance = null; load.Token = null;
        }
    }
}
