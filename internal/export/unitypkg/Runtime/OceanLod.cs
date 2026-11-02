// Level-of-detail selection for a world built by the Ocean Compiler. Pure C#: no UnityEngine types, so the same code is
// unit-tested outside Unity (tools/lodtests). The Unity glue (OceanLodStreamer) only converts positions and loads tiles.
//
// Model: the tiles form a quadtree. Tile (L, x, y) is covered by the four tiles (L-1, 2x..2x+1, 2y..2y+1); x grows east,
// y grows north, ids are "L<level>_<x>_<y>" (docs/formats/world-v2.md). A selection is a set of leaves ("a cut") that
// covers the area once. Three independent steps:
//   1. OceanLodSelector.Select   which leaves should be shown for the cameras (distance rule, hysteresis, 2:1 balance)
//   2. OceanLodCover.Compute     which loaded tiles to draw while the wanted ones are still loading (no holes, no overlap)
//   3. OceanLodCover.SkirtSides  which tile edges border a tile of another level (those need the crack-hiding skirt)
using System;
using System.Collections.Generic;

namespace OceanCompiler.World
{
    /// <summary>A position in Unity space (x east, y up, z north), metres.</summary>
    public readonly struct OceanPoint
    {
        public readonly float x, y, z;
        public OceanPoint(float x, float y, float z) { this.x = x; this.y = y; this.z = z; }
    }

    /// <summary>One tile of the pyramid in Unity space.</summary>
    public readonly struct OceanLodTile
    {
        public readonly string id;
        public readonly int level, gx, gy;           // grid cell at this level
        public readonly float minX, minZ, maxX, maxZ; // horizontal extent, metres
        public OceanLodTile(string id, int level, int gx, int gy, float minX, float minZ, float maxX, float maxZ)
        { this.id = id; this.level = level; this.gx = gx; this.gy = gy; this.minX = minX; this.minZ = minZ; this.maxX = maxX; this.maxZ = maxZ; }

        /// <summary>Parses "L&lt;level&gt;_&lt;x&gt;_&lt;y&gt;" (x and y may be negative).</summary>
        public static bool TryParseId(string id, out int level, out int gx, out int gy)
        {
            level = gx = gy = 0;
            if (string.IsNullOrEmpty(id) || id[0] != 'L') return false;
            var p = id.Split('_');
            return p.Length == 3 && int.TryParse(p[0].Substring(1), out level) && int.TryParse(p[1], out gx) && int.TryParse(p[2], out gy) && level >= 0;
        }
    }

    /// <summary>The tiles of a world as a quadtree. Tiles that are absent (a level that was not imported, or a tile with no valid sample) are simply missing.</summary>
    public sealed class OceanLodTree
    {
        public sealed class Node
        {
            public OceanLodTile tile;
            public Node parent;
            public readonly Node[] children = new Node[4]; // slot = (x & 1) + 2 * (y & 1); null when that child does not exist
            public int childCount;
            public float distance; // metres to the nearest camera at the last Select (for ordering)
            public string Id => tile.id;
            public int Level => tile.level;
        }

        readonly Dictionary<(int, int, int), Node> byKey = new Dictionary<(int, int, int), Node>();
        readonly Dictionary<string, Node> byId = new Dictionary<string, Node>();
        public readonly List<Node> roots = new List<Node>();
        public int MaxLevel { get; private set; }
        public int Count => byId.Count;
        public IEnumerable<Node> Nodes => byId.Values;

        /// <summary>Duplicate ids or duplicate cells keep the first tile.</summary>
        public OceanLodTree(IEnumerable<OceanLodTile> tiles)
        {
            foreach (var t in tiles)
            {
                if (byId.ContainsKey(t.id) || byKey.ContainsKey((t.level, t.gx, t.gy))) continue;
                var n = new Node { tile = t };
                byId[t.id] = n; byKey[(t.level, t.gx, t.gy)] = n;
                if (t.level > MaxLevel) MaxLevel = t.level;
            }
            foreach (var n in byId.Values)
            {
                var t = n.tile;
                if (byKey.TryGetValue((t.level + 1, t.gx >> 1, t.gy >> 1), out var p))
                {
                    n.parent = p;
                    int slot = (t.gx & 1) + 2 * (t.gy & 1);
                    if (p.children[slot] == null) { p.children[slot] = n; p.childCount++; }
                }
                else roots.Add(n);
            }
            roots.Sort((a, b) => string.CompareOrdinal(a.tile.id, b.tile.id)); // deterministic regardless of dictionary order
        }

        public Node Get(string id) => id != null && byId.TryGetValue(id, out var n) ? n : null;
        public Node At(int level, int gx, int gy) => byKey.TryGetValue((level, gx, gy), out var n) ? n : null;
        public bool IsAncestor(Node ancestor, Node n) { for (var p = n.parent; p != null; p = p.parent) if (p == ancestor) return true; return false; }
    }

    /// <summary>Tunables of the selection. All distances are metres in the horizontal plane (see docs/unity-lod.md).</summary>
    [Serializable]
    public sealed class OceanLodSettings
    {
        /// <summary>A tile is replaced by its children when the nearest camera is closer than splitFactor x the tile's span. Scales with the world's tile size.</summary>
        public float splitFactor = 2f;
        /// <summary>Optional explicit split distance per level (index = level of the tile that would be split). Missing or non-positive entries use splitFactor.</summary>
        public float[] splitDistancesM;
        /// <summary>A tile that is already split stays split until the camera is farther than the split distance x (1 + hysteresis). Prevents flicker at the boundary.</summary>
        public float hysteresis = 0.25f;
        /// <summary>Tiles farther than this from every camera are not selected at all. 0 = no limit.</summary>
        public float viewDistanceM = 0;
        /// <summary>Largest allowed level difference between neighbouring leaves. 0 = unlimited.</summary>
        public int maxLevelDelta = 1;
        /// <summary>Finest level that may be shown (tiles at this level are never split). 0 = all levels.</summary>
        public int minLevel = 0;
        /// <summary>Coarsest level that may be shown: tiles above it are always split, as far as their children exist.</summary>
        public int maxLevel = int.MaxValue;

        public float SplitDistance(OceanLodTree.Node n)
        {
            int l = n.tile.level;
            if (splitDistancesM != null && l < splitDistancesM.Length && splitDistancesM[l] > 0) return splitDistancesM[l];
            return splitFactor * (n.tile.maxX - n.tile.minX);
        }
    }

    public static class OceanLodSelector
    {
        /// <summary>Horizontal distance from a point to a tile rectangle (0 inside).</summary>
        public static float Distance(in OceanLodTile t, in OceanPoint p)
        {
            float dx = Math.Max(Math.Max(t.minX - p.x, 0f), p.x - t.maxX), dz = Math.Max(Math.Max(t.minZ - p.z, 0f), p.z - t.maxZ);
            return (float)Math.Sqrt(dx * dx + dz * dz);
        }

        static float Nearest(in OceanLodTile t, IReadOnlyList<OceanPoint> cameras)
        {
            float d = float.MaxValue;
            for (int i = 0; i < cameras.Count; i++) d = Math.Min(d, Distance(t, cameras[i]));
            return d;
        }

        /// <summary>
        /// Chooses the leaves to show for the cameras. <paramref name="previous"/> is the previous result (hysteresis; may be null).
        /// <paramref name="result"/> is cleared and filled nearest first. Several cameras: a tile uses the distance to the nearest one.
        /// No cameras: nothing is selected. <paramref name="unavailable"/> (may be null) lists tiles that cannot be loaded (a load failed): a tile
        /// is not split into a child that is unavailable, so the parent keeps covering that ground instead of leaving a hole.
        /// </summary>
        public static void Select(OceanLodTree tree, IReadOnlyList<OceanPoint> cameras, OceanLodSettings s, ICollection<OceanLodTree.Node> previous, List<OceanLodTree.Node> result,
            ICollection<OceanLodTree.Node> unavailable = null)
        {
            result.Clear();
            if (cameras == null || cameras.Count == 0) return;
            foreach (var r in tree.roots) Visit(r, cameras, s, previous, result, unavailable);
            if (s.maxLevelDelta > 0) Balance(tree, s.maxLevelDelta, result, unavailable);
            foreach (var n in result) n.distance = Nearest(n.tile, cameras);
            result.Sort((a, b) => { int c = a.distance.CompareTo(b.distance); return c != 0 ? c : string.CompareOrdinal(a.tile.id, b.tile.id); });
        }

        static void Visit(OceanLodTree.Node n, IReadOnlyList<OceanPoint> cameras, OceanLodSettings s, ICollection<OceanLodTree.Node> previous, List<OceanLodTree.Node> result, ICollection<OceanLodTree.Node> unavailable)
        {
            float d = Nearest(n.tile, cameras);
            n.distance = d;
            if (s.viewDistanceM > 0 && d > s.viewDistanceM) return;
            bool mustSplit = n.tile.level > s.maxLevel;
            bool canSplit = n.childCount > 0 && n.tile.level > s.minLevel && !AnyChildUnavailable(n, unavailable);
            bool split = false;
            if (canSplit)
            {
                float threshold = s.SplitDistance(n);
                // a tile shown as a leaf last time splits at the threshold; one that was already split stays split a little longer
                if (previous != null && !previous.Contains(n)) threshold *= 1f + Math.Max(0f, s.hysteresis);
                split = mustSplit || d < threshold;
            }
            if (!split) { result.Add(n); return; }
            foreach (var c in n.children) if (c != null) Visit(c, cameras, s, previous, result, unavailable);
        }

        // Splits every leaf that is more than maxDelta levels coarser than a neighbouring leaf, until none is. Splits only go finer, so it ends.
        static bool AnyChildUnavailable(OceanLodTree.Node n, ICollection<OceanLodTree.Node> unavailable)
        {
            if (unavailable == null || unavailable.Count == 0) return false;
            foreach (var c in n.children) if (c != null && unavailable.Contains(c)) return true;
            return false;
        }

        // Scratch reused by every call so a steady-state step does not allocate them. Not thread-safe: the selector is called from the main thread.
        static readonly HashSet<OceanLodTree.Node> balanceSet = new HashSet<OceanLodTree.Node>();
        static readonly List<OceanLodTree.Node> balanceWork = new List<OceanLodTree.Node>();

        static void Balance(OceanLodTree tree, int maxDelta, List<OceanLodTree.Node> leaves, ICollection<OceanLodTree.Node> unavailable)
        {
            var set = balanceSet; set.Clear(); foreach (var l in leaves) set.Add(l);
            var work = balanceWork; work.Clear();
            bool changed = true;
            while (changed)
            {
                changed = false;
                work.Clear(); work.AddRange(set);
                foreach (var leaf in work)
                {
                    if (!set.Contains(leaf)) continue;
                    var t = leaf.tile;
                    for (int side = 0; side < 4; side++)
                    {
                        int nx = t.gx + (side == 1 ? 1 : side == 3 ? -1 : 0), ny = t.gy + (side == 0 ? 1 : side == 2 ? -1 : 0);
                        for (int m = t.level + maxDelta + 1; m <= tree.MaxLevel; m++)
                        {
                            var coarse = tree.At(m, nx >> (m - t.level), ny >> (m - t.level));
                            if (coarse == null || !set.Contains(coarse) || coarse.childCount == 0 || AnyChildUnavailable(coarse, unavailable)) continue;
                            set.Remove(coarse);
                            foreach (var c in coarse.children) if (c != null) set.Add(c);
                            changed = true;
                        }
                    }
                }
            }
            leaves.Clear(); leaves.AddRange(set);
        }
    }

    [Flags] public enum OceanSides { None = 0, North = 1, East = 2, South = 4, West = 8 }

    public static class OceanLodCover
    {
        /// <summary>
        /// Which loaded tiles to draw. Every wanted tile that is loaded is drawn; one that is not yet loaded is covered by its nearest loaded
        /// ancestor, or else by its loaded descendants, so the ground never has a hole while something that covers it is in memory.
        /// An ancestor that is drawn hides its descendants: the finer tiles replace it together, once all of them that are wanted are loaded.
        /// <paramref name="visible"/> is cleared and filled. Wanted tiles that nothing covers yet are added to <paramref name="uncovered"/> (may be null).
        /// </summary>
        public static void Compute(OceanLodTree tree, ICollection<OceanLodTree.Node> wanted, ISet<OceanLodTree.Node> loaded, HashSet<OceanLodTree.Node> visible, List<OceanLodTree.Node> uncovered)
        {
            visible.Clear(); uncovered?.Clear();
            foreach (var d in wanted)
            {
                if (loaded.Contains(d)) { visible.Add(d); continue; }
                OceanLodTree.Node a = d.parent;
                while (a != null && !loaded.Contains(a)) a = a.parent;
                if (a != null) { visible.Add(a); continue; }
                if (!AddLoadedBelow(d, loaded, visible)) uncovered?.Add(d);
            }
            // an ancestor hides everything below it
            var drop = dropScratch; drop.Clear();
            foreach (var v in visible)
                for (var p = v.parent; p != null; p = p.parent) if (visible.Contains(p)) { drop.Add(v); break; }
            foreach (var v in drop) visible.Remove(v);
        }

        static readonly List<OceanLodTree.Node> dropScratch = new List<OceanLodTree.Node>(); // reused; main thread only

        static bool AddLoadedBelow(OceanLodTree.Node n, ISet<OceanLodTree.Node> loaded, ICollection<OceanLodTree.Node> visible)
        {
            bool any = false;
            foreach (var c in n.children)
            {
                if (c == null) continue;
                if (loaded.Contains(c)) { visible.Add(c); any = true; }
                else any |= AddLoadedBelow(c, loaded, visible);
            }
            return any;
        }

        /// <summary>True when no drawn tile lies inside another: the invariant of Compute.</summary>
        public static bool IsDisjoint(OceanLodTree tree, IEnumerable<OceanLodTree.Node> visible)
        {
            var list = new List<OceanLodTree.Node>(visible);
            for (int i = 0; i < list.Count; i++)
                for (int j = i + 1; j < list.Count; j++)
                    if (tree.IsAncestor(list[i], list[j]) || tree.IsAncestor(list[j], list[i])) return false;
            return true;
        }

        /// <summary>
        /// The edges of <paramref name="n"/> whose neighbour is drawn at another level (coarser, or finer in whole or in part). Those edges get the skirt;
        /// edges next to a same-level tile are stitched by Unity's own terrain neighbours, edges with nothing drawn beside them need nothing.
        /// </summary>
        public static OceanSides SkirtSides(OceanLodTree tree, ISet<OceanLodTree.Node> visible, OceanLodTree.Node n)
        {
            var t = n.tile; var sides = OceanSides.None;
            for (int side = 0; side < 4; side++)
            {
                int nx = t.gx + (side == 1 ? 1 : side == 3 ? -1 : 0), ny = t.gy + (side == 0 ? 1 : side == 2 ? -1 : 0);
                var same = tree.At(t.level, nx, ny);
                bool differs = false;
                if (same != null && visible.Contains(same)) continue;
                if (same != null) differs = AnyVisibleBelow(same, visible, side);
                for (int m = t.level + 1; !differs && m <= tree.MaxLevel; m++)
                {
                    var coarse = tree.At(m, nx >> (m - t.level), ny >> (m - t.level));
                    if (coarse != null && visible.Contains(coarse)) differs = true;
                }
                if (differs) sides |= (OceanSides)(1 << side);
            }
            return sides;
        }

        // Is any visible descendant of the neighbouring cell on the half that faces us? (ourSide: 0 N, 1 E, 2 S, 3 W)
        static bool AnyVisibleBelow(OceanLodTree.Node cell, ISet<OceanLodTree.Node> visible, int ourSide)
        {
            foreach (var c in cell.children)
            {
                if (c == null) continue;
                int cx = c.tile.gx & 1, cy = c.tile.gy & 1;
                bool touches = ourSide == 0 ? cy == 0 : ourSide == 2 ? cy == 1 : ourSide == 1 ? cx == 0 : cx == 1;
                if (!touches) continue;
                if (visible.Contains(c) || AnyVisibleBelow(c, visible, ourSide)) return true;
            }
            return false;
        }
    }
}
