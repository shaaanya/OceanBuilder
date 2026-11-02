// Property tests for OceanLod.cs. No test framework: every check prints on failure and the exit code is the failure count.
using System;
using System.Collections.Generic;
using System.Linq;
using OceanBuilder.World;

static class T
{
    static int failures, checks;
    static void Check(bool ok, string what) { checks++; if (!ok) { failures++; Console.WriteLine("FAIL " + what); } }

    // A world of nx x ny level-0 tiles (span metres each) with levels 0..levels-1; keep(level,x,y) = false drops a tile.
    static OceanLodTree World(int nx, int ny, float span, int levels, Func<int, int, int, bool> keep = null, int x0 = 0, int y0 = 0)
    {
        var list = new List<OceanLodTile>();
        for (int l = 0; l < levels; l++)
        {
            float s = span * (1 << l);
            for (int gy = y0 >> l; gy <= (y0 + ny - 1) >> l; gy++) for (int gx = x0 >> l; gx <= (x0 + nx - 1) >> l; gx++)
            {
                if (keep != null && !keep(l, gx, gy)) continue;
                list.Add(new OceanLodTile("L" + l + "_" + gx + "_" + gy, l, gx, gy, gx * s, gy * s, (gx + 1) * s, (gy + 1) * s));
            }
        }
        return new OceanLodTree(list);
    }

    static double Area(IEnumerable<OceanLodTree.Node> ns) => ns.Sum(n => (double)(n.tile.maxX - n.tile.minX) * (n.tile.maxZ - n.tile.minZ));
    static bool Touch(OceanLodTile a, OceanLodTile b) // share an edge segment
    {
        bool xs = a.maxX == b.minX || b.maxX == a.minX, zs = a.maxZ == b.minZ || b.maxZ == a.minZ;
        bool xo = a.minX < b.maxX && b.minX < a.maxX, zo = a.minZ < b.maxZ && b.minZ < a.maxZ;
        return (xs && zo) || (zs && xo);
    }

    static void Main()
    {
        var rnd = new Random(1234);
        Ids();
        Cut(rnd);
        Allocation();
        Hysteresis();
        Missing();
        MultiCamera();
        Streaming(rnd);
        Skirts();
        Console.WriteLine("lodtests: " + checks + " checks, " + failures + " failures");
        Environment.Exit(failures);
    }

    static void Ids()
    {
        Check(OceanLodTile.TryParseId("L3_-12_7", out var l, out var x, out var y) && l == 3 && x == -12 && y == 7, "id parse negative");
        Check(!OceanLodTile.TryParseId("X1_2_3", out _, out _, out _) && !OceanLodTile.TryParseId("L1_2", out _, out _, out _) && !OceanLodTile.TryParseId("", out _, out _, out _), "id parse garbage");
        var tree = World(8, 8, 100, 4, null, -8, -8); // negative grid coordinates: x >> 1 must floor
        Check(tree.roots.Count == 1 && tree.roots[0].Level == 3, "negative grid builds one root, got " + tree.roots.Count);
        Check(tree.Nodes.All(n => n.Level == 3 || n.parent != null && n.parent.Level == n.Level + 1), "every non-root has a parent");
    }

    // Steady state: the same camera, previous cut and loaded set every step, as the streamer calls it. Reports bytes allocated per
    // Select + Cover.Compute step on the calling thread after a warm-up; the streamer runs these at most every updateIntervalSeconds (0.1).
    static void Allocation()
    {
        var tree = World(16, 16, 256, 5);
        var s = new OceanLodSettings { splitFactor = 1.5f };
        var cams = new[] { new OceanPoint(2000, 0, 2000), new OceanPoint(500, 0, 3500) };
        var sel = new List<OceanLodTree.Node>(); var prev = new HashSet<OceanLodTree.Node>(); var loaded = new HashSet<OceanLodTree.Node>();
        var vis = new HashSet<OceanLodTree.Node>(); var unc = new List<OceanLodTree.Node>();
        void Step()
        {
            OceanLodSelector.Select(tree, cams, s, prev, sel);
            prev.Clear(); foreach (var n in sel) prev.Add(n);
            loaded.Clear(); foreach (var n in sel) loaded.Add(n);
            OceanLodCover.Compute(tree, sel, loaded, vis, unc);
        }
        for (int i = 0; i < 300; i++) Step();
        long before = GC.GetAllocatedBytesForCurrentThread();
        const int N = 2000;
        for (int i = 0; i < N; i++) Step();
        double per = (GC.GetAllocatedBytesForCurrentThread() - before) / (double)N;
        Console.WriteLine("steady-state allocation: " + per.ToString("F0") + " bytes per Select+Cover step (" + sel.Count + " leaves, " + tree.Count + " tiles)");
        Check(per < 256, "steady-state step allocates under 256 bytes, got " + per.ToString("F0"));
    }

    static void Cut(Random rnd)
    {
        foreach (var delta in new[] { 0, 1, 2 })
        {
            var tree = World(16, 16, 256, 5);
            var s = new OceanLodSettings { maxLevelDelta = delta, splitFactor = 1.5f };
            var sel = new List<OceanLodTree.Node>();
            for (int i = 0; i < 300; i++)
            {
                var cams = new[] { new OceanPoint((float)(rnd.NextDouble() * 5000 - 500), 0, (float)(rnd.NextDouble() * 5000 - 500)) };
                OceanLodSelector.Select(tree, cams, s, null, sel);
                Check(Math.Abs(Area(sel) - 16 * 256.0 * 16 * 256) < 1, "cut covers the world exactly once (delta " + delta + ")");
                Check(OceanLodCover.IsDisjoint(tree, sel), "leaves disjoint");
                Check(sel.Select(n => n.Id).Distinct().Count() == sel.Count, "no duplicate leaf");
                for (int a = 0; a < sel.Count; a++)
                    for (int b = a + 1; b < sel.Count; b++)
                        if (delta > 0 && Touch(sel[a].tile, sel[b].tile) && Math.Abs(sel[a].Level - sel[b].Level) > delta) { Check(false, "balance violated " + sel[a].Id + " " + sel[b].Id); a = b = sel.Count; }
                for (int k = 1; k < sel.Count; k++) if (sel[k - 1].distance > sel[k].distance) { Check(false, "sorted nearest first"); break; }
                // the finest tile is the one under the camera
                var under = sel.First(); Check(under.distance == 0 || sel.All(n => n.distance >= under.distance), "nearest first");
            }
            // closer camera never gives a coarser tile under it
            var far = new List<OceanLodTree.Node>(); var near = new List<OceanLodTree.Node>();
            OceanLodSelector.Select(tree, new[] { new OceanPoint(2000, 0, 2000) }, s, null, far);
            OceanLodSelector.Select(tree, new[] { new OceanPoint(2040, 0, 2040) }, s, null, near);
            Check(near.Min(n => n.Level) == 0, "level 0 next to the camera");
            OceanLodSelector.Select(tree, new[] { new OceanPoint(-1e6f, 0, -1e6f) }, s, null, far);
            Check(far.Count == 1 && far[0].Level == 4, "a camera very far away selects the single coarsest tile, got " + far.Count);
        }
        // thresholds that would put level 0 next to level 4: the balance step must split the coarse tiles
        {
            var tree = World(16, 16, 256, 5);
            var cams = new[] { new OceanPoint(1020, 0, 500) };
            var sel = new List<OceanLodTree.Node>();
            var jumpy = new OceanLodSettings { splitDistancesM = new float[] { 1, 1, 1, 1, 1e9f }, maxLevelDelta = 0 };
            OceanLodSelector.Select(tree, cams, jumpy, null, sel);
            int worst = 0; foreach (var a in sel) foreach (var b in sel) if (Touch(a.tile, b.tile)) worst = Math.Max(worst, Math.Abs(a.Level - b.Level));
            Check(worst >= 2, "test setup: unbalanced cut has a big jump, got " + worst);
            foreach (var d in new[] { 1, 2 })
            {
                jumpy.maxLevelDelta = d;
                OceanLodSelector.Select(tree, cams, jumpy, null, sel);
                worst = 0; foreach (var a in sel) foreach (var b in sel) if (Touch(a.tile, b.tile)) worst = Math.Max(worst, Math.Abs(a.Level - b.Level));
                Check(worst <= d && Math.Abs(Area(sel) - 4096.0 * 4096) < 1, "balance limits the jump to " + d + ", got " + worst);
            }
        }
        // explicit distances and level clamps
        var t2 = World(8, 8, 100, 4);
        var sel2 = new List<OceanLodTree.Node>();
        OceanLodSelector.Select(t2, new[] { new OceanPoint(400, 0, 400) }, new OceanLodSettings { minLevel = 2, maxLevelDelta = 0 }, null, sel2);
        Check(sel2.Min(n => n.Level) == 2, "minLevel stops splitting");
        OceanLodSelector.Select(t2, new[] { new OceanPoint(400, 0, 400) }, new OceanLodSettings { maxLevel = 1, maxLevelDelta = 0, splitDistancesM = new float[] { 0, 0, 0, 0 } }, null, sel2);
        Check(sel2.Max(n => n.Level) <= 1, "maxLevel forces splitting");
        OceanLodSelector.Select(t2, new[] { new OceanPoint(400, 0, 400) }, new OceanLodSettings { splitFactor = 0.001f, maxLevelDelta = 0, splitDistancesM = new float[] { 0, 0, 1e9f, 0 } }, null, sel2);
        Check(sel2.All(n => n.Level != 3) && sel2.Any(n => n.Level == 1), "explicit distance for level 2 splits it, factor handles the rest");
        OceanLodSelector.Select(t2, new OceanPoint[0], new OceanLodSettings(), null, sel2);
        Check(sel2.Count == 0, "no camera selects nothing");
        OceanLodSelector.Select(t2, new[] { new OceanPoint(0, 0, 0) }, new OceanLodSettings { viewDistanceM = 150, maxLevelDelta = 0 }, null, sel2);
        Check(sel2.Count > 0 && sel2.All(n => n.distance <= 150), "view distance culls");
    }

    static void Hysteresis()
    {
        var tree = World(4, 1, 100, 3);
        var s = new OceanLodSettings { splitFactor = 1f, hysteresis = 0.25f, maxLevelDelta = 0 };
        var sel = new List<OceanLodTree.Node>(); var prev = new HashSet<OceanLodTree.Node>();
        // level-2 tile spans 400 m (x 0..400); split distance 400 m. Walk the camera away along z and back, count flips of the tile's state.
        int flips = 0; bool? last = null;
        for (int rep = 0; rep < 5; rep++)
            foreach (var z in new float[] { 395, 405, 395, 405, 450, 495, 505, 395, 405 })
            {
                OceanLodSelector.Select(tree, new[] { new OceanPoint(200, 0, 400 + z) }, s, prev, sel);
                bool split = !sel.Any(n => n.Level == 2);
                if (last.HasValue && last.Value != split) flips++;
                last = split; prev.Clear(); foreach (var n in sel) prev.Add(n);
            }
        // without hysteresis the same walk flips on every crossing
        int flips0 = 0; last = null; prev.Clear(); s.hysteresis = 0;
        for (int rep = 0; rep < 5; rep++)
            foreach (var z in new float[] { 395, 405, 395, 405, 450, 495, 505, 395, 405 })
            {
                OceanLodSelector.Select(tree, new[] { new OceanPoint(200, 0, 400 + z) }, s, prev, sel);
                bool split = !sel.Any(n => n.Level == 2);
                if (last.HasValue && last.Value != split) flips0++;
                last = split; prev.Clear(); foreach (var n in sel) prev.Add(n);
            }
        Check(flips < flips0 && flips <= 12, "hysteresis reduces flips: " + flips + " vs " + flips0 + " without");
    }

    static void Missing()
    {
        // level 1 absent entirely (not imported): level 2 tiles are the parents of nothing; level-0 tiles become roots
        var tree = World(8, 8, 100, 4, (l, x, y) => l != 1);
        var sel = new List<OceanLodTree.Node>();
        OceanLodSelector.Select(tree, new[] { new OceanPoint(50, 0, 50) }, new OceanLodSettings(), null, sel);
        Check(sel.Count > 0 && sel.All(n => n.Level == 0 || n.Level >= 2), "level 1 missing: selection still valid");
        // a tile with no data (omitted) is simply not selected
        var hole = World(4, 4, 100, 3, (l, x, y) => !(l == 0 && x == 1 && y == 1));
        OceanLodSelector.Select(hole, new[] { new OceanPoint(150, 0, 150) }, new OceanLodSettings { splitFactor = 5 }, null, sel);
        Check(sel.All(n => n.Id != "L0_1_1") && Area(sel) == 16 * 100.0 * 100 - 100.0 * 100, "omitted tile leaves a gap of exactly its area");
        // a child that cannot be loaded keeps its parent as the leaf
        {
            var t3 = World(4, 4, 100, 3);
            var bad = new HashSet<OceanLodTree.Node> { t3.Get("L0_1_1") };
            OceanLodSelector.Select(t3, new[] { new OceanPoint(150, 0, 150) }, new OceanLodSettings { splitFactor = 5 }, null, sel, bad);
            Check(sel.All(n => n.Id != "L0_1_1" && n.Id != "L0_0_0") && sel.Any(n => n.Id == "L1_0_0"), "unavailable child: parent stays a leaf, got " + string.Join(",", sel.Select(n => n.Id)));
            Check(Math.Abs(Area(sel) - 16 * 100.0 * 100) < 1, "unavailable child: still a full cut");
        }
        // only level 0 imported
        var flat = World(4, 4, 100, 1);
        OceanLodSelector.Select(flat, new[] { new OceanPoint(0, 0, 0) }, new OceanLodSettings { viewDistanceM = 120 }, null, sel);
        Check(sel.Count == 3 && sel.All(n => n.Level == 0), "level-0 world: radius selection, got " + sel.Count);
    }

    static void MultiCamera()
    {
        var tree = World(16, 16, 100, 5);
        var s = new OceanLodSettings { maxLevelDelta = 0, splitFactor = 1 };
        var a = new List<OceanLodTree.Node>(); var b = new List<OceanLodTree.Node>(); var both = new List<OceanLodTree.Node>();
        var c1 = new OceanPoint(100, 0, 100); var c2 = new OceanPoint(1500, 0, 1500);
        OceanLodSelector.Select(tree, new[] { c1 }, s, null, a); OceanLodSelector.Select(tree, new[] { c2 }, s, null, b); OceanLodSelector.Select(tree, new[] { c1, c2 }, s, null, both);
        Check(both.Any(n => n.Level == 0 && n.tile.minX < 200) && both.Any(n => n.Level == 0 && n.tile.minX > 1300), "both cameras get detail");
        Check(Math.Abs(Area(both) - 1600.0 * 1600) < 1, "two cameras still a single cut");
    }

    // A random walk of the camera with asynchronous loading in random order: the cover never has a hole where it had none, and never overlaps.
    static void Streaming(Random rnd)
    {
        var tree = World(16, 16, 100, 5);
        var s = new OceanLodSettings { splitFactor = 1.2f };
        var prev = new HashSet<OceanLodTree.Node>(); var loaded = new HashSet<OceanLodTree.Node>(); var loading = new List<OceanLodTree.Node>();
        var visible = new HashSet<OceanLodTree.Node>(); var uncovered = new List<OceanLodTree.Node>(); var wanted = new List<OceanLodTree.Node>();
        var covered = new bool[16, 16]; var wasCovered = new bool[16, 16];
        float x = 800, z = 800; int maxLoaded = 0, steps = 3000, holesAfterWarm = 0;
        for (int step = 0; step < steps; step++)
        {
            // rapid movement: jumps now and then
            if (rnd.NextDouble() < 0.02) { x = (float)(rnd.NextDouble() * 1600); z = (float)(rnd.NextDouble() * 1600); }
            else { x = Math.Clamp(x + (float)(rnd.NextDouble() - 0.5) * 120, 0, 1600); z = Math.Clamp(z + (float)(rnd.NextDouble() - 0.5) * 120, 0, 1600); }
            OceanLodSelector.Select(tree, new[] { new OceanPoint(x, 0, z) }, s, prev, wanted);
            prev.Clear(); foreach (var n in wanted) prev.Add(n);
            // some loads complete (random order, 0..3 per step); loads that are no longer wanted are dropped from the queue
            loading.RemoveAll(n => !prev.Contains(n) && !prev.Any(w => tree.IsAncestor(n, w) || tree.IsAncestor(w, n)));
            foreach (var n in wanted) if (!loaded.Contains(n) && !loading.Contains(n)) loading.Add(n);
            for (int k = rnd.Next(4); k > 0 && loading.Count > 0; k--) { int i = rnd.Next(loading.Count); loaded.Add(loading[i]); loading.RemoveAt(i); }
            OceanLodCover.Compute(tree, wanted, loaded, visible, uncovered);
            Check(OceanLodCover.IsDisjoint(tree, visible), "cover disjoint at step " + step);
            // unload what is neither wanted nor drawn
            loaded.RemoveWhere(n => !prev.Contains(n) && !visible.Contains(n));
            OceanLodCover.Compute(tree, wanted, loaded, visible, uncovered); // unloading must not change what is drawn
            maxLoaded = Math.Max(maxLoaded, loaded.Count);
            Array.Clear(covered, 0, covered.Length);
            foreach (var v in visible)
                for (int cy = (int)(v.tile.minZ / 100); cy < (int)(v.tile.maxZ / 100); cy++)
                    for (int cx = (int)(v.tile.minX / 100); cx < (int)(v.tile.maxX / 100); cx++) covered[cx, cy] = true;
            if (step > 200)
                for (int cy = 0; cy < 16; cy++) for (int cx = 0; cx < 16; cx++)
                    if (wasCovered[cx, cy] && !covered[cx, cy]) holesAfterWarm++;
            Array.Copy(covered, wasCovered, covered.Length);
        }
        Check(holesAfterWarm == 0, "no cell that was covered ever becomes a hole: " + holesAfterWarm);
        Check(maxLoaded < tree.Count, "never everything loaded at once: " + maxLoaded + " of " + tree.Count);
        // with everything wanted loaded, the cover equals the wanted cut
        OceanLodSelector.Select(tree, new[] { new OceanPoint(300, 0, 300) }, s, null, wanted);
        loaded.Clear(); foreach (var n in wanted) loaded.Add(n);
        OceanLodCover.Compute(tree, wanted, loaded, visible, uncovered);
        Check(visible.SetEquals(wanted) && uncovered.Count == 0, "all loaded: cover == cut");
        // a parent that is still loaded covers children that are not
        var parent = wanted.First(n => n.Level == 0).parent;
        loaded.Clear(); loaded.Add(parent);
        OceanLodCover.Compute(tree, wanted, loaded, visible, uncovered);
        Check(visible.Count == 1 && visible.Contains(parent) && uncovered.Count == wanted.Count(n => !tree.IsAncestor(parent, n) && n != parent), "parent covers its not-yet-loaded children");
    }

    static void Skirts()
    {
        var tree = World(4, 4, 100, 3);
        var vis = new HashSet<OceanLodTree.Node>();
        // fine tiles in the SW quadrant, one coarse level-1 tile east of them, level-1 tiles elsewhere
        foreach (var id in new[] { "L0_0_0", "L0_1_0", "L0_0_1", "L0_1_1", "L1_1_0", "L1_0_1", "L1_1_1" }) vis.Add(tree.Get(id));
        var fine = tree.Get("L0_1_0");
        var sides = OceanLodCover.SkirtSides(tree, vis, fine);
        Check(sides == OceanSides.East, "fine tile next to a coarse one: only the east edge has a skirt, got " + sides);
        var coarse = tree.Get("L1_1_0");
        sides = OceanLodCover.SkirtSides(tree, vis, coarse);
        Check(sides == OceanSides.West, "coarse tile next to finer ones: west edge only (north neighbour is level 1, others absent), got " + sides);
        var inner = tree.Get("L0_0_0");
        Check(OceanLodCover.SkirtSides(tree, vis, inner) == OceanSides.None, "same-level neighbours and nothing beyond the edge: no skirt");
        var north = tree.Get("L1_0_1");
        sides = OceanLodCover.SkirtSides(tree, vis, north);
        Check(sides == OceanSides.South, "level-1 tile above two level-0 tiles: south edge, got " + sides);
    }
}
