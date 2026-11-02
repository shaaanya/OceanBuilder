// Streams a world's tiles at the level of detail that suits the viewers, at runtime. Put it on an object at the world origin
// (or any parent: positions are taken in this object's local space, so a floating origin that moves the parent works),
// assign the index, and name the viewers (cameras, or any transforms); with none assigned Camera.main is used.
//
// Behaviour (docs/unity-lod.md has the model and the measurements):
//   * Selection   OceanLodSelector: a tile is replaced by its four children when the nearest viewer is closer than splitFactor x its span
//                 (or an explicit distance per level); hysteresis stops flicker; neighbouring tiles differ by at most maxLevelDelta levels.
//   * Loading     at most maxInFlight loads at once, nearest and uncovered first. A tile is never loaded twice at the same time: a load
//                 that is no longer wanted is flagged and released when it completes, and un-flagged if the viewer turns back.
//   * Transitions a tile that is replaced stays drawn until everything that replaces it is loaded (no hole); only one of a tile and
//                 its descendants is drawn (no overlap). Skirts hide the gap beside a neighbour of another level.
//   * Failures    a failed load is retried with a growing delay, at most maxRetries times; meanwhile the parent tile is kept
//                 instead of splitting. The failure is reported through TileFailed.
//   * Memory      loaded tiles that are no longer needed are released after unloadDelaySeconds; nothing else is kept.
using System;
using System.Collections.Generic;
using UnityEngine;

namespace OceanCompiler.World
{
    [Serializable]
    public struct OceanLodStats
    {
        public int wanted, loaded, loading, visible, uncovered, failedTiles;
        public int loadsStarted, loadsCompleted, loadsFailed, loadsCancelled, unloads;
        public int[] loadedPerLevel, visiblePerLevel;
    }

    public class OceanLodStreamer : MonoBehaviour
    {
        public OceanWorldIndex index;
        [Tooltip("Cameras or any transforms the world is streamed for. A tile uses the distance to the nearest one. Empty: Camera.main.")]
        public List<Transform> viewers = new List<Transform>();
        public OceanLodSettings lod = new OceanLodSettings();
        [Tooltip("Tile loads in flight at once.")]
        public int maxInFlight = 2;
        [Tooltip("Seconds between selection updates (a finished load also triggers one).")]
        public float updateIntervalSeconds = 0.1f;
        [Tooltip("A tile nothing needs any more is kept this long before it is released, so a viewer that turns back does not reload it.")]
        public float unloadDelaySeconds = 2f;
        [Tooltip("Seconds before a failed tile is tried again; doubles with each failure.")]
        public float retryAfterSeconds = 5;
        [Tooltip("After this many retries a tile is given up on (until Retry() is called).")]
        public int maxRetries = 3;
        [Tooltip("Terrain colliders are enabled on drawn tiles up to this level (0 = only the finest). Coarse tiles are not accurate enough to stand on.")]
        public int collidersMaxLevel = 0;
        [Tooltip("Show the skirts that hide cracks beside a neighbour of another level.")]
        public bool skirts = true;

        public event Action<OceanTileEntry, Terrain> TileLoaded;
        public event Action<OceanTileEntry> TileUnloaded;
        /// <summary>A load failed. final = it will not be retried automatically.</summary>
        public event Action<OceanTileEntry, string, bool> TileFailed;
        /// <summary>A tile started or stopped being drawn.</summary>
        public event Action<OceanTileEntry, bool> TileShown;

        sealed class Slot
        {
            public OceanLodTree.Node node; public OceanTileEntry entry; public OceanTileLoad load;
            public Terrain terrain; public TerrainCollider collider; public OceanTileSkirts skirts;
            public bool done, cancel, shown; public float lastNeeded;
        }
        struct Failure { public int count; public float retryAt; public bool final; }

        IOceanTileLoader loader;
        OceanLodTree tree;
        readonly Dictionary<OceanLodTree.Node, Slot> slots = new Dictionary<OceanLodTree.Node, Slot>();
        readonly Dictionary<OceanLodTree.Node, Failure> failures = new Dictionary<OceanLodTree.Node, Failure>();
        readonly HashSet<OceanLodTree.Node> loadedSet = new HashSet<OceanLodTree.Node>(), wantedSet = new HashSet<OceanLodTree.Node>(), visible = new HashSet<OceanLodTree.Node>(),
            unavailable = new HashSet<OceanLodTree.Node>(), needed = new HashSet<OceanLodTree.Node>();
        readonly List<OceanLodTree.Node> wanted = new List<OceanLodTree.Node>(), uncovered = new List<OceanLodTree.Node>(), scratch = new List<OceanLodTree.Node>();
        readonly List<OceanPoint> points = new List<OceanPoint>();
        int pending;      // loads whose callback has not run yet (includes cancelled ones)
        bool dirty = true;
        float nextUpdate, lastNow;
        OceanLodStats stats;

        /// <summary>The loader in use. Assign before the first update to replace the default (Addressables when installed).</summary>
        public IOceanTileLoader Loader { get => loader; set => loader = value; }
        public OceanLodTree Tree => tree;
        public OceanLodStats Stats { get { stats.wanted = wanted.Count; stats.loaded = loadedSet.Count; stats.loading = pending; stats.visible = visible.Count; stats.uncovered = uncovered.Count; return stats; } }
        public bool IsLoaded(string id) { var n = tree?.Get(id); return n != null && loadedSet.Contains(n); }
        public bool IsVisible(string id) { var n = tree?.Get(id); return n != null && visible.Contains(n); }
        public Terrain GetTerrain(string id) { var n = tree?.Get(id); return n != null && slots.TryGetValue(n, out var s) && s.done ? s.terrain : null; }
        public IEnumerable<string> VisibleIds { get { foreach (var n in visible) yield return n.Id; } }
        /// <summary>Wanted tiles that nothing loaded covers yet (holes in the ground right now).</summary>
        public int UncoveredCount => uncovered.Count;

        void OnEnable() { dirty = true; }

        void Update() { loader?.Tick(); }

        void LateUpdate()
        {
            if (index == null) return;
            if (!dirty && Time.unscaledTime < nextUpdate) return;
            CollectViewers();
            Step(points, Time.unscaledTime);
        }

        void CollectViewers()
        {
            points.Clear();
            foreach (var v in viewers) if (v != null) Add(v.position);
            if (points.Count == 0 && Camera.main != null) Add(Camera.main.transform.position);
        }

        void Add(Vector3 world) { var p = transform.InverseTransformPoint(world); points.Add(new OceanPoint(p.x, p.y, p.z)); }

        /// <summary>Forget all failures and try the failed tiles again at the next update.</summary>
        public void Retry() { failures.Clear(); dirty = true; }

        /// <summary>One streaming step for viewers at the given positions (this object's local space) at time <paramref name="now"/>. LateUpdate calls it; tests call it directly.</summary>
        public void Step(IReadOnlyList<OceanPoint> viewerPoints, float now)
        {
            if (index == null) return;
            if (tree == null) tree = index.BuildLodTree();
            if (loader == null) loader = OceanTileLoaders.DefaultFactory != null ? OceanTileLoaders.DefaultFactory() : null;
            if (loader == null) { Debug.LogWarning("OceanLodStreamer: no tile loader (install Addressables or assign Loader)"); enabled = false; return; }
            dirty = false; lastNow = now; nextUpdate = now + Mathf.Max(0.01f, updateIntervalSeconds);

            unavailable.Clear();
            foreach (var kv in failures) if (kv.Value.final || now < kv.Value.retryAt) unavailable.Add(kv.Key);
            OceanLodSelector.Select(tree, viewerPoints, lod, wantedSet, wanted, unavailable);
            wantedSet.Clear(); foreach (var n in wanted) wantedSet.Add(n);
            OceanLodCover.Compute(tree, wanted, loadedSet, visible, uncovered);

            needed.Clear(); foreach (var n in wantedSet) needed.Add(n); foreach (var n in visible) needed.Add(n);
            foreach (var kv in slots)
                if (needed.Contains(kv.Key)) { kv.Value.lastNeeded = now; kv.Value.cancel = false; }

            // release what nothing needs any more (after the delay); a load still in flight is flagged and released when it completes
            scratch.Clear();
            foreach (var kv in slots) if (!needed.Contains(kv.Key) && now - kv.Value.lastNeeded >= unloadDelaySeconds) scratch.Add(kv.Key);
            foreach (var n in scratch)
            {
                var s = slots[n];
                if (!s.done) { s.cancel = true; continue; }
                Unload(s);
            }

            // start loads: holes first, then nearest first (wanted is sorted by distance)
            foreach (var n in uncovered) { if (pending >= maxInFlight) break; TryBegin(n); }
            foreach (var n in wanted) { if (pending >= maxInFlight) break; TryBegin(n); }

            Apply();
        }

        void TryBegin(OceanLodTree.Node n)
        {
            if (slots.ContainsKey(n) || unavailable.Contains(n)) return;
            var entry = index.Get(n.Id);
            if (entry == null) return;
            var slot = new Slot { node = n, entry = entry, lastNeeded = lastNow };
            slots[n] = slot;
            pending++; stats.loadsStarted++;
            slot.load = loader.Begin(entry, transform, load => OnDone(slot, load));
        }

        void OnDone(Slot slot, OceanTileLoad load)
        {
            pending--;
            dirty = true;
            if (this == null) { loader.Release(load); return; }
            if (!load.Failed && load.Instance != null)
            {
                slot.terrain = load.Instance.GetComponent<Terrain>();
                if (slot.terrain == null) { load.Failed = true; load.Error = slot.entry.id + " has no Terrain component"; }
            }
            if (load.Failed || load.Instance == null)
            {
                slots.Remove(slot.node);
                loader.Release(load);
                stats.loadsFailed++;
                failures.TryGetValue(slot.node, out var f);
                f.count++; f.retryAt = lastNow + retryAfterSeconds * Mathf.Pow(2, f.count - 1); f.final = f.count > maxRetries;
                failures[slot.node] = f;
                Debug.LogWarning("OceanLodStreamer: loading " + slot.entry.id + " failed (" + (load.Error ?? "unknown") + ")" + (f.final ? "; giving up" : "; retry in " + (f.retryAt - lastNow).ToString("F0") + " s"));
                TileFailed?.Invoke(slot.entry, load.Error, f.final);
                return;
            }
            failures.Remove(slot.node);
            if (slot.cancel) { slots.Remove(slot.node); loader.Release(load); stats.loadsCancelled++; return; }
            slot.done = true;
            slot.collider = slot.terrain.GetComponent<TerrainCollider>();
            slot.skirts = load.Instance.GetComponent<OceanTileSkirts>();
            SetShown(slot, false); // drawn only once the cover says so, in this frame's LateUpdate
            loadedSet.Add(slot.node);
            stats.loadsCompleted++;
            index.LinkNeighbours(slot.entry, slot.terrain, GetTerrain);
            TileLoaded?.Invoke(slot.entry, slot.terrain);
        }

        void Unload(Slot s)
        {
            slots.Remove(s.node);
            loadedSet.Remove(s.node);
            if (s.shown) { s.shown = false; TileShown?.Invoke(s.entry, false); }
            loader.Release(s.load);
            stats.unloads++;
            index.LinkNeighbours(s.entry, null, GetTerrain); // neighbours drop their references to the terrain that is going away
            TileUnloaded?.Invoke(s.entry);
        }

        void SetShown(Slot s, bool show)
        {
            if (s.terrain) s.terrain.drawHeightmap = show;
            if (s.collider) s.collider.enabled = show && s.entry.level <= collidersMaxLevel;
            if (!show && s.skirts) s.skirts.Apply(OceanSides.None);
        }

        void Apply()
        {
            foreach (var kv in slots)
            {
                var s = kv.Value;
                if (!s.done) continue;
                bool show = visible.Contains(kv.Key);
                if (show != s.shown)
                {
                    s.shown = show;
                    SetShown(s, show);
                    TileShown?.Invoke(s.entry, show);
                }
                if (show && s.skirts) s.skirts.Apply(skirts ? OceanLodCover.SkirtSides(tree, visible, kv.Key) : OceanSides.None);
            }
            int levels = tree.MaxLevel + 1; // histograms for Stats (allocated once)
            if (stats.loadedPerLevel == null || stats.loadedPerLevel.Length != levels) { stats.loadedPerLevel = new int[levels]; stats.visiblePerLevel = new int[levels]; }
            Array.Clear(stats.loadedPerLevel, 0, levels); Array.Clear(stats.visiblePerLevel, 0, levels);
            foreach (var n in loadedSet) stats.loadedPerLevel[n.Level]++;
            foreach (var n in visible) stats.visiblePerLevel[n.Level]++;
            int g = 0; foreach (var f in failures.Values) if (f.final) g++;
            stats.failedTiles = g;
        }

        /// <summary>Releases every tile; the streamer can be used again afterwards.</summary>
        public void Clear()
        {
            foreach (var s in slots.Values) loader?.Release(s.load);
            slots.Clear(); loadedSet.Clear(); visible.Clear(); wantedSet.Clear(); wanted.Clear(); uncovered.Clear(); failures.Clear();
            pending = 0; dirty = true;
        }

        void OnDestroy() { Clear(); }
    }
}
