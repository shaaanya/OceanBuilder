// Loads and unloads tile prefabs through Addressables around a target (the ship or the camera).
// Put it on an object at the world origin, assign the index and the target. Needs the world's
// Addressables groups to be built (Ocean > Addressables > Build And Check).
// Limitations: no floating-origin handling (far from the origin Unity float precision degrades); terrains that
// were linked to a tile keep a stale neighbour reference after it unloads (Unity treats it as missing).
using System;
using System.Collections.Generic;
using UnityEngine;
using UnityEngine.AddressableAssets;
using UnityEngine.ResourceManagement.AsyncOperations;

namespace OceanBuilder.World
{
    public class OceanTileStreamer : MonoBehaviour
    {
        public OceanWorldIndex index;
        public Transform target;
        [Tooltip("Tiles whose rectangle is closer than this to the target are loaded.")]
        public float loadRadiusM = 8000;
        [Tooltip("Loaded tiles farther than this are unloaded (kept at least loadRadiusM).")]
        public float unloadRadiusM = 10000;
        [Tooltip("Tile loads that may be in flight at once.")]
        public int maxInFlight = 2;

        public event Action<OceanTileEntry, Terrain> TileLoaded;
        public event Action<OceanTileEntry> TileUnloaded;

        class Slot
        {
            public OceanTileEntry entry;
            public AsyncOperationHandle<GameObject> handle;
            public Terrain terrain;
            public bool loading = true, cancelled;
        }

        [Tooltip("Seconds before a tile whose load failed is tried again.")]
        public float retryAfterSeconds = 10;

        readonly Dictionary<string, Slot> slots = new Dictionary<string, Slot>();
        readonly Dictionary<string, float> retryAt = new Dictionary<string, float>();
        readonly List<OceanTileEntry> toLoad = new List<OceanTileEntry>();
        readonly List<string> toUnload = new List<string>();
        int inFlight;

        public int LoadedCount { get { int n = 0; foreach (var s in slots.Values) if (!s.loading) n++; return n; } }
        public int PendingCount => inFlight;
        public bool IsLoaded(string tileId) => slots.TryGetValue(tileId, out var s) && !s.loading;
        public Terrain GetTerrain(string tileId) => slots.TryGetValue(tileId, out var s) && !s.loading ? s.terrain : null;

        void Update()
        {
            if (index == null || target == null) return;
            Tick(transform.InverseTransformPoint(target.position));
        }

        /// <summary>One streaming step for a position in this object's local space (Unity world space when it sits at the origin).</summary>
        public void Tick(Vector3 position)
        {
            OceanStreamingPlan.Compute(index, position, loadRadiusM, unloadRadiusM, slots.Keys, toLoad, toUnload);
            foreach (var id in toUnload) Unload(id);
            foreach (var e in toLoad)
            {
                if (inFlight >= maxInFlight) break;
                if (retryAt.TryGetValue(e.id, out var when) && Time.unscaledTime < when) continue;
                Load(e);
            }
        }

        void Load(OceanTileEntry e)
        {
            var slot = new Slot { entry = e };
            slots[e.id] = slot;
            inFlight++;
            slot.handle = Addressables.InstantiateAsync(e.address, transform, false);
            slot.handle.Completed += h => OnLoaded(slot, h);
        }

        void OnLoaded(Slot slot, AsyncOperationHandle<GameObject> h)
        {
            inFlight--;
            slot.loading = false;
            if (h.Status != AsyncOperationStatus.Succeeded)
            {
                Debug.LogError("OceanTileStreamer: loading " + slot.entry.address + " failed: " + h.OperationException);
                slots.Remove(slot.entry.id);
                retryAt[slot.entry.id] = Time.unscaledTime + retryAfterSeconds;
                return;
            }
            retryAt.Remove(slot.entry.id);
            if (slot.cancelled || this == null)
            {
                Addressables.ReleaseInstance(h.Result);
                slots.Remove(slot.entry.id);
                return;
            }
            slot.terrain = h.Result.GetComponent<Terrain>();
            index.LinkNeighbours(slot.entry, slot.terrain, id => GetTerrain(id));
            TileLoaded?.Invoke(slot.entry, slot.terrain);
        }

        void Unload(string id)
        {
            if (!slots.TryGetValue(id, out var slot)) return;
            if (slot.loading) { slot.cancelled = true; return; } // released when the load finishes
            slots.Remove(id);
            Addressables.ReleaseInstance(slot.handle);
            TileUnloaded?.Invoke(slot.entry);
        }

        void OnDestroy()
        {
            foreach (var s in slots.Values)
            {
                s.cancelled = true;
                if (!s.loading) Addressables.ReleaseInstance(s.handle);
            }
            slots.Clear();
        }
    }
}
