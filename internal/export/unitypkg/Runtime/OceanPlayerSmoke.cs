// Runs inside a built player and prints one line per phase, then quits (exit code 0 only when every check passed).
// Added to a scene copy by OceanStandaloneBuild; it is also an example of querying an imported world at runtime.
//   OCEAN_PLAYER_SMOKE   level 0: every imported terrain exists and its centre height lies in the tile's manifest range
//   OCEAN_PLAYER_LOD     every imported coarser tile (index.lodTiles): instantiated from the player's own data, position,
//                        size and resolution against the index (which the importer filled from the manifest's bounds_utm),
//                        heights inside the manifest range, shared edges with same-level neighbours bit-equal, vertices
//                        against the level-0 terrains (exact for vertex decimation), unload releases the terrains
//   OCEAN_PLAYER_ADDRESSABLES   (OceanPlayerSmokeAddressables, when Addressables is present) the same tiles loaded by address
using System.Collections;
using System.Collections.Generic;
using System.Text;
using UnityEngine;

namespace OceanCompiler.World
{
    /// <summary>Collects failures of a smoke run; each phase adds to it.</summary>
    public class OceanSmokeReport
    {
        public int failures;
        public readonly StringBuilder detail = new StringBuilder();
        public void Fail(string message) { failures++; if (detail.Length < 1500) detail.Append(' ').Append(message).Append(';'); }
        public void Check(bool ok, string message) { if (!ok) Fail(message); }
    }

    /// <summary>An extra smoke phase on the same GameObject (the Addressables one lives in a separate assembly).</summary>
    public interface IOceanSmokePhase
    {
        IEnumerator Run(OceanWorldIndex index, OceanSmokeReport report);
    }

    public class OceanPlayerSmoke : MonoBehaviour
    {
        public OceanWorldIndex index;
        public float toleranceM = 0.5f;
        [Tooltip("Prefabs of the coarser LOD tiles (index.lodTiles); set by OceanStandaloneBuild so the player carries them.")]
        public GameObject[] lodPrefabs = new GameObject[0];

        [Tooltip("The player quits with exit code 4 after this many seconds if a phase never finishes.")]
        public float watchdogSeconds = 90;

        IEnumerator Start()
        {
            var report = new OceanSmokeReport();
            StartCoroutine(Watchdog(report));
            Guard(() => Level0(report), "level0", report);
            yield return Safe(LodPhase(report), "lod", report);
            foreach (var phase in GetComponents<IOceanSmokePhase>()) yield return Safe(phase.Run(index, report), phase.GetType().Name, report);
            string shot = CommandLineArg("-oceanShot");
            if (shot != null && !Application.isBatchMode || shot != null && SystemInfo.graphicsDeviceType != UnityEngine.Rendering.GraphicsDeviceType.Null)
                yield return Screenshot(shot, report);
            Debug.Log("OCEAN_PLAYER_RESULT failures=" + report.failures + report.detail);
            Application.Quit(report.failures == 0 ? 0 : 3);
        }

        static string CommandLineArg(string name)
        {
            var a = System.Environment.GetCommandLineArgs();
            for (int i = 0; i < a.Length - 1; i++) if (a[i] == name) return a[i + 1];
            return null;
        }

        /// <summary>With -oceanShot &lt;png&gt; (needs a graphics device, so no -nographics): looks at the world from the south-west
        /// and saves the rendered frame, to show that the imported terrains actually draw.</summary>
        IEnumerator Screenshot(string path, OceanSmokeReport report)
        {
            var cam = Camera.main ?? FindFirstObjectByType<Camera>();
            if (cam == null) { report.Fail("no camera for the screenshot"); yield break; }
            float w = index.tilesX * index.tileSpanM, d = index.tilesY * index.tileSpanM;
            var centre = new Vector3(w / 2, 0, d / 2);
            cam.transform.position = centre + new Vector3(-w * 0.55f, Mathf.Max(w, d) * 0.45f, -d * 0.75f);
            cam.transform.LookAt(centre);
            cam.farClipPlane = Mathf.Max(w, d) * 4;
            yield return null; yield return null;
            yield return new WaitForEndOfFrame();
            var tex = ScreenCapture.CaptureScreenshotAsTexture();
            System.IO.File.WriteAllBytes(path, tex.EncodeToPNG());
            // a frame that is one colour (nothing drawn, or an error shader everywhere) is a failure
            var px = tex.GetPixels32(); int distinct = 0; var seen = new HashSet<int>();
            for (int i = 0; i < px.Length && distinct < 64; i += 97) if (seen.Add(px[i].r << 16 | px[i].g << 8 | px[i].b)) distinct++;
            Debug.Log("OCEAN_PLAYER_SHOT " + path + " " + tex.width + "x" + tex.height + " distinct_colours_sampled=" + distinct);
            report.Check(distinct > 16, "the frame has only " + distinct + " distinct colours");
            Destroy(tex);
        }

        IEnumerator Watchdog(OceanSmokeReport report)
        {
            yield return new WaitForSecondsRealtime(watchdogSeconds);
            Debug.Log("OCEAN_PLAYER_RESULT failures=" + (report.failures + 1) + " watchdog: a phase did not finish in " + watchdogSeconds + " s" + report.detail);
            Application.Quit(4);
        }

        static void Guard(System.Action a, string phase, OceanSmokeReport report)
        {
            try { a(); } catch (System.Exception e) { report.Fail(phase + " threw " + e.GetType().Name + ": " + e.Message); }
        }

        /// <summary>Runs a phase to the end; an exception in it is a failure, not a hang.</summary>
        static IEnumerator Safe(IEnumerator phase, string name, OceanSmokeReport report)
        {
            while (true)
            {
                object current;
                try { if (!phase.MoveNext()) yield break; current = phase.Current; }
                catch (System.Exception e) { report.Fail(name + " threw " + e.GetType().Name + ": " + e.Message); yield break; }
                yield return current;
            }
        }

        void Level0(OceanSmokeReport report)
        {
            var sb = new StringBuilder();
            int ok = 0, bad = 0, noTerrain = 0, empty = 0;
            foreach (var t in index.tiles)
            {
                if (t.level != 0) continue;
                var terrain = Level0Terrain(t.id);
                if (terrain == null) { noTerrain++; continue; }
                var centre = terrain.transform.position + new Vector3(t.size.x / 2, 0, t.size.z / 2);
                float h = terrain.SampleHeight(centre) + terrain.transform.position.y;
                // the tile's elevation range is known from the manifest: the sample must lie inside it
                // nodata cells are flat at the world minimum (documented importer behaviour), which can lie below the tile's own minimum
                float lowest = t.nodataFraction > 0f ? Mathf.Min(t.minM, index.minM) : t.minM;
                if (t.nodataFraction >= 1f) empty++;
                if (h >= lowest - toleranceM && h <= t.maxM + toleranceM) ok++;
                else { bad++; sb.Append(' ').Append(t.id).Append('=').Append(h.ToString("F1")); }
            }
            Debug.Log("OCEAN_PLAYER_SMOKE world=" + index.worldName + " build=" + index.buildId.Substring(0, 12) +
                      " tiles=" + index.tiles.Length + " ok=" + ok + " outside_range=" + bad + " missing_terrain=" + noTerrain + " empty_tiles=" + empty + sb);
            report.Check(bad == 0 && noTerrain == 0 && ok > 0, "level0 ok=" + ok + " bad=" + bad + " missing=" + noTerrain);
        }

        readonly Dictionary<string, Terrain> level0 = new Dictionary<string, Terrain>();
        Terrain Level0Terrain(string id)
        {
            if (level0.TryGetValue(id, out var cached)) return cached;
            var go = GameObject.Find(id);
            var t = go ? go.GetComponentInChildren<Terrain>() : null;
            level0[id] = t;
            return t;
        }

        /// <summary>Level-0 height at a Unity-space point, or NaN outside the imported tiles.</summary>
        float Level0Height(float x, float z)
        {
            float span = index.tileSpanM;
            if (x < 0 || z < 0 || x > index.tilesX * span || z > index.tilesY * span) return float.NaN;
            int c = Mathf.Min(Mathf.FloorToInt(x / span), index.tilesX - 1), r = Mathf.Min(Mathf.FloorToInt(z / span), index.tilesY - 1);
            foreach (var t in index.tiles)
                if (t.col == c && t.row == index.tilesY - 1 - r)
                {
                    var terrain = Level0Terrain(t.id);
                    return terrain ? terrain.SampleHeight(new Vector3(x, 0, z)) + terrain.transform.position.y : float.NaN;
                }
            return float.NaN;
        }

        IEnumerator LodPhase(OceanSmokeReport report)
        {
            int n = index.lodTiles.Length;
            var prefabs = new Dictionary<string, GameObject>();
            foreach (var p in lodPrefabs) if (p != null) prefabs[p.name] = p;
            var sb = new StringBuilder();
            int checkedTiles = 0, missing = 0, vertices = 0, exact = 0;
            float worstFine = 0, worstEdge = 0, worstCrack = 0;
            int before = FindObjectsByType<Terrain>(FindObjectsSortMode.None).Length;
            var inst = new Dictionary<string, Terrain>();
            int res0 = 0;
            foreach (var t in index.tiles) { var tt = Level0Terrain(t.id); if (tt) { res0 = tt.terrainData.heightmapResolution; break; } }
            bool strict = index.lodMethod == "vertex-decimation" || string.IsNullOrEmpty(index.lodMethod);
            float quant = Mathf.Max(1f, index.maxM - index.minM) / 65535f;
            foreach (var e in index.lodTiles)
            {
                if (!prefabs.TryGetValue(e.id, out var prefab)) { missing++; report.Fail("no prefab for " + e.id); continue; }
                var go = Instantiate(prefab);
                var terrain = go.GetComponent<Terrain>();
                if (terrain == null) { report.Fail(e.id + " has no Terrain"); continue; }
                inst[e.id] = terrain;
                checkedTiles++;
                var td = terrain.terrainData;
                var pos = terrain.transform.position;
                report.Check((pos - e.position).magnitude < 0.01f, e.id + " position " + pos + " != index " + e.position);
                // the index position was derived from the manifest's bounds_utm: east/north of the tile's SW corner
                report.Check(Mathf.Abs((float)(index.originEasting + pos.x - e.minEasting)) < 0.05f && Mathf.Abs((float)(index.originNorthingSouth + pos.z - e.minNorthing)) < 0.05f, e.id + " not at manifest bounds");
                report.Check(Mathf.Abs(td.size.x - (float)(e.maxEasting - e.minEasting)) < 0.05f && Mathf.Abs(td.size.z - (float)(e.maxNorthing - e.minNorthing)) < 0.05f, e.id + " size " + td.size + " != bounds");
                report.Check(td.heightmapResolution == res0 && res0 > 0, e.id + " resolution " + td.heightmapResolution + " != level 0 " + res0);
                report.Check(e.size == td.size, e.id + " entry size differs from the terrain");
                int S = td.heightmapResolution;
                var h = td.GetHeights(0, 0, S, S); // h[z, x], normalised 0..1 of td.size.y
                float lo = float.MaxValue, hi = float.MinValue;
                float step = td.size.x / (S - 1);
                for (int z = 0; z < S; z++)
                    for (int x = 0; x < S; x++)
                    {
                        float m = h[z, x] * td.size.y + pos.y;
                        lo = Mathf.Min(lo, m); hi = Mathf.Max(hi, m);
                        // against level 0: a coarse vertex sits on a level-0 vertex
                        if ((x % 4 == 0 && z % 4 == 0) || x == 0 || z == 0 || x == S - 1 || z == S - 1)
                        {
                            float f = Level0Height(pos.x + x * step, pos.z + z * step);
                            if (float.IsNaN(f)) continue;
                            vertices++;
                            float d = Mathf.Abs(m - f);
                            if (strict) { worstFine = Mathf.Max(worstFine, d); if (d <= Mathf.Max(0.1f, 3 * quant)) exact++; }
                            else worstFine = Mathf.Max(worstFine, d);
                        }
                    }
                report.Check(hi <= e.maxM + toleranceM && (lo >= e.minM - toleranceM || e.nodataFraction > 0), e.id + " heights " + lo.ToString("F1") + ".." + hi.ToString("F1") + " outside manifest " + e.minM.ToString("F1") + ".." + e.maxM.ToString("F1"));
                // midpoints between coarse vertices vs level 0: the crack a mixed-level seam would show
                if (e.level == 1)
                    for (int x = 1; x < S - 1; x += 8)
                    {
                        float c = terrain.SampleHeight(new Vector3(pos.x + x * step + step / 2, 0, pos.z + (S / 2) * step)) + pos.y;
                        float f = Level0Height(pos.x + x * step + step / 2, pos.z + (S / 2) * step);
                        if (!float.IsNaN(f)) worstCrack = Mathf.Max(worstCrack, Mathf.Abs(c - f));
                    }
            }
            // shared edges of same-level neighbours are bit-equal (tiles share their edge samples)
            int pairs = 0;
            foreach (var e in index.lodTiles)
            {
                if (!inst.TryGetValue(e.id, out var a)) continue;
                int S = a.terrainData.heightmapResolution;
                if (!string.IsNullOrEmpty(e.east) && inst.TryGetValue(e.east, out var east))
                {
                    var ha = a.terrainData.GetHeights(S - 1, 0, 1, S); var hb = east.terrainData.GetHeights(0, 0, 1, S);
                    for (int z = 0; z < S; z++) worstEdge = Mathf.Max(worstEdge, Mathf.Abs(ha[z, 0] - hb[z, 0]) * a.terrainData.size.y);
                    pairs++;
                }
                if (!string.IsNullOrEmpty(e.north) && inst.TryGetValue(e.north, out var north))
                {
                    var ha = a.terrainData.GetHeights(0, S - 1, S, 1); var hb = north.terrainData.GetHeights(0, 0, S, 1);
                    for (int x = 0; x < S; x++) worstEdge = Mathf.Max(worstEdge, Mathf.Abs(ha[0, x] - hb[0, x]) * a.terrainData.size.y);
                    pairs++;
                }
            }
            report.Check(worstEdge < 0.001f, "neighbour edge mismatch " + worstEdge);
            if (strict) report.Check(vertices == 0 || exact == vertices, "coarse vertices off level 0: worst " + worstFine.ToString("F3") + " m, " + (vertices - exact) + " of " + vertices);
            int during = FindObjectsByType<Terrain>(FindObjectsSortMode.None).Length;
            report.Check(during == before + checkedTiles, "terrains while loaded " + during + ", expected " + (before + checkedTiles));
            // unload: destroy the instances and the terrain data they pulled in must be collectable
            foreach (var t in inst.Values) Destroy(t.gameObject);
            yield return null;
            yield return Resources.UnloadUnusedAssets();
            int after = FindObjectsByType<Terrain>(FindObjectsSortMode.None).Length;
            report.Check(after == before, "terrains after unload " + after + ", expected " + before);
            // levels the world does not have: no tiles, no exception
            report.Check(!new List<OceanTileEntry>(index.LodTiles(index.maxLevelImported + 1)).GetEnumerator().MoveNext(), "tiles reported for an unimported level");
            report.Check(!new List<OceanTileEntry>(index.LodTiles(99)).GetEnumerator().MoveNext(), "tiles reported for level 99");
            Debug.Log("OCEAN_PLAYER_LOD method=" + (strict ? "vertex-decimation" : index.lodMethod) + " max_level=" + index.maxLevelImported + " tiles=" + n + " instantiated=" + checkedTiles + " missing_prefab=" + missing +
                      " vertices_vs_level0=" + vertices + " within_tol=" + (strict ? exact : -1) + " worst_vs_level0_m=" + worstFine.ToString("F3") + " edge_pairs=" + pairs + " worst_edge_m=" + worstEdge.ToString("F4") +
                      " level1_midpoint_crack_m=" + worstCrack.ToString("F2") + " terrains_before=" + before + " during=" + during + " after=" + after + sb);
        }
    }
}
