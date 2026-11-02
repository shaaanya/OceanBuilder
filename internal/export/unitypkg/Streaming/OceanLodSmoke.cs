// Runtime LOD streaming scenarios, run by the player smoke (OceanStandaloneBuild adds this component) and by the PlayMode test of the
// same name, so the Editor and a built player are checked by one piece of code. Every scenario uses the real OceanLodStreamer with the
// real Addressables loader, wrapped only to count and to inject failures:
//   calm        a viewer in a corner: mixed levels, the drawn tiles cover the ground once, skirts on exactly the mixed edges, not every tile loaded
//   move        the viewer crosses the world: the old detail goes, the new arrives, covering stays complete
//   rapid       teleports every frame, then back and forth: tiles never overlap, loads never exceed the limit, no tile loaded twice at once
//   interrupt   loads abandoned half way: after settling, instances alive == tiles loaded (nothing leaked)
//   failure     a tile that cannot be loaded: reported, its parent keeps covering, it loads again after the failure is gone
//   corrupt     an index entry whose address does not exist (real Addressables failure): reported, the rest of the world streams
//   levels      an index without level 1, and without any coarse level: still a valid cover
//   release     destroying the streamer releases every instance and every Addressables reference
//   seam        (with a graphics device) cracks between tiles of different sizes counted by rendering: the real tiles and synthetic data
using System;
using System.Collections;
using System.Collections.Generic;
using System.Text;
using UnityEngine;
using UnityEngine.AddressableAssets;

namespace OceanCompiler.World
{
    /// <summary>Wraps a loader: counts what the streamer asks for and can make chosen tiles fail.</summary>
    public sealed class OceanCountingLoader : IOceanTileLoader
    {
        const string Injected = "injected failure";
        readonly IOceanTileLoader inner;
        readonly List<(OceanTileLoad, Action<OceanTileLoad>)> failing = new List<(OceanTileLoad, Action<OceanTileLoad>)>();
        public readonly HashSet<string> fail = new HashSet<string>();
        public readonly Dictionary<string, int> live = new Dictionary<string, int>(), begins = new Dictionary<string, int>();
        public int begun, released, inFlight, maxInFlight, maxLivePerId, liveTotal;

        public OceanCountingLoader(IOceanTileLoader inner) { this.inner = inner; }

        public OceanTileLoad Begin(OceanTileEntry entry, Transform parent, Action<OceanTileLoad> onDone)
        {
            begun++; inFlight++; maxInFlight = Math.Max(maxInFlight, inFlight);
            begins[entry.id] = begins.TryGetValue(entry.id, out var b) ? b + 1 : 1;
            live[entry.id] = live.TryGetValue(entry.id, out var l) ? l + 1 : 1; liveTotal++;
            maxLivePerId = Math.Max(maxLivePerId, live[entry.id]);
            if (fail.Contains(entry.id))
            {
                var load = new OceanTileLoad { entry = entry, Failed = true, IsDone = true, Error = Injected };
                failing.Add((load, onDone));
                return load;
            }
            return inner.Begin(entry, parent, x => { inFlight--; onDone(x); });
        }

        public void Release(OceanTileLoad load)
        {
            if (load == null) return;
            released++; liveTotal--; live[load.entry.id]--;
            if (load.Error != Injected) inner.Release(load);
        }

        public void Tick()
        {
            inner.Tick();
            if (failing.Count == 0) return;
            var run = failing.ToArray(); failing.Clear();
            foreach (var (load, cb) in run) { inFlight--; cb(load); }
        }
    }

    public class OceanLodSmoke : MonoBehaviour, IOceanSmokePhase
    {
        [Tooltip("Frames a wait may take before it is a failure.")]
        public int maxFramesPerWait = 3000;

        static float Area(IEnumerable<OceanLodTree.Node> ns) { double a = 0; foreach (var n in ns) a += (double)(n.tile.maxX - n.tile.minX) * (n.tile.maxZ - n.tile.minZ); return (float)a; }

        // Batch-mode frames can be far shorter than the streamer's delays: wait in real time.
        static IEnumerator Pass(float seconds) { float end = Time.unscaledTime + seconds; while (Time.unscaledTime < end) yield return null; }

        IEnumerator Settle(OceanLodStreamer st, OceanSmokeReport report, string what)
        {
            int stable = 0;
            for (int i = 0; i < maxFramesPerWait; i++)
            {
                yield return null;
                var s = st.Stats;
                if (s.loading == 0 && st.UncoveredCount == 0 && s.wanted > 0) { if (++stable >= 6) yield break; } else stable = 0;
            }
            var f = st.Stats;
            report.Fail("timeout settling '" + what + "': loading=" + f.loading + " uncovered=" + st.UncoveredCount + " wanted=" + f.wanted);
        }

        static OceanPoint P(Transform t) { var p = t.position; return new OceanPoint(p.x, p.y, p.z); }

        static List<OceanLodTree.Node> Drawn(OceanLodStreamer st) { var l = new List<OceanLodTree.Node>(); foreach (var id in st.VisibleIds) l.Add(st.Tree.Get(id)); return l; }

        // the drawn tiles do not overlap; with a complete world they also cover as much ground as a fresh selection does
        static void CheckCover(OceanLodStreamer st, OceanWorldIndex index, Transform viewer, OceanSmokeReport report, string tag, bool compareArea = true)
        {
            var drawn = Drawn(st);
            report.Check(OceanLodCover.IsDisjoint(st.Tree, drawn), tag + ": a tile is drawn together with one of its descendants");
            var exp = new List<OceanLodTree.Node>();
            OceanLodSelector.Select(st.Tree, new[] { P(viewer) }, st.lod, null, exp);
            if (compareArea && index.tiles.Length == index.tilesX * index.tilesY)
            {
                float a = Area(drawn), e = Area(exp);
                report.Check(Mathf.Abs(a - e) <= e * 1e-5f, tag + ": ground covered " + a.ToString("F0") + " m2, expected " + e.ToString("F0") + " m2");
            }
        }

        static void CheckSkirts(OceanLodStreamer st, OceanSmokeReport report, string tag, out int withSkirt)
        {
            withSkirt = 0;
            var vis = new HashSet<OceanLodTree.Node>(Drawn(st));
            foreach (var n in vis)
            {
                var sk = st.GetTerrain(n.Id)?.GetComponent<OceanTileSkirts>();
                if (sk == null) continue;
                var want = st.skirts ? OceanLodCover.SkirtSides(st.Tree, vis, n) : OceanSides.None;
                if (want != OceanSides.None) withSkirt++;
                report.Check(sk.Current == want, tag + ": " + n.Id + " skirts " + sk.Current + ", expected " + want);
            }
        }

        public IEnumerator Run(OceanWorldIndex index, OceanSmokeReport report)
        {
            var init = Addressables.InitializeAsync(false);
            yield return init;
            if (init.IsValid()) Addressables.Release(init);
            int maxLevel = index.maxLevelImported;
            float span = index.tileSpanM, W = index.tilesX * span, D = index.tilesY * span;
            // the imported scene already holds the level-0 terrains: hide them while the streamer works, so only streamed tiles are measured
            var sceneRoot = GameObject.Find("Ocean_" + index.worldName);
            bool hadRoot = sceneRoot != null && sceneRoot.activeSelf;
            if (hadRoot) sceneRoot.SetActive(false);
            var sea = GameObject.Find("SeaLevel"); bool hadSea = sea != null && sea.activeSelf;
            if (hadSea) sea.SetActive(false);
            int terrainsBefore = FindObjectsByType<Terrain>(FindObjectsSortMode.None).Length;

            var viewer = new GameObject("LodViewer").transform;
            var counting = new OceanCountingLoader(new OceanAddressablesTileLoader());
            var go = new GameObject("LodStreamer");
            var st = go.AddComponent<OceanLodStreamer>();
            st.index = index; st.viewers.Add(viewer); st.Loader = counting;
            st.maxInFlight = 2; st.unloadDelaySeconds = 0.3f; st.retryAfterSeconds = 0.3f; st.updateIntervalSeconds = 0.02f; st.maxRetries = 2;
            st.lod.splitFactor = 0.6f;
            var failed = new List<(string id, bool final)>(); st.TileFailed += (e, msg, final) => failed.Add((e.id, final));
            var sb = new StringBuilder();
            bool mixed = maxLevel > 0;

            // ---- calm: a viewer near the south-west corner
            viewer.position = new Vector3(span * 0.1f, 0, span * 0.1f);
            yield return Settle(st, report, "calm");
            if (st.Tree == null) { report.Fail("streamer built no tree"); yield break; }
            CheckCover(st, index, viewer, report, "calm");
            CheckSkirts(st, report, "calm", out int withSkirt);
            var s1 = st.Stats;
            int levelsDrawn = 0; for (int l = 0; s1.visiblePerLevel != null && l < s1.visiblePerLevel.Length; l++) if (s1.visiblePerLevel[l] > 0) levelsDrawn++;
            if (mixed)
            {
                report.Check(levelsDrawn >= 2, "calm: only " + levelsDrawn + " level drawn with " + (maxLevel + 1) + " imported");
                report.Check(s1.loaded < st.Tree.Count, "calm: all " + st.Tree.Count + " tiles loaded");
                report.Check(withSkirt > 0, "calm: mixed levels but no skirt shown");
            }
            report.Check(counting.maxLivePerId <= 1, "calm: a tile was loaded twice at once (" + counting.maxLivePerId + ")");
            report.Check(counting.begun == s1.loadsStarted, "calm: loader saw " + counting.begun + " loads, streamer started " + s1.loadsStarted);
            sb.Append(" calm_loaded=").Append(s1.loaded).Append("/").Append(st.Tree.Count).Append(" levels_drawn=").Append(levelsDrawn).Append(" skirts=").Append(withSkirt);
            // a coarse tile must not be solid ground
            string nearOld = null;
            foreach (var id in st.VisibleIds)
            {
                var t = st.GetTerrain(id); var e = index.Get(id);
                if (t == null || e == null) continue;
                if (e.level == 0 && nearOld == null) nearOld = id;
                var col = t.GetComponent<TerrainCollider>();
                report.Check(col == null || col.enabled == (e.level <= st.collidersMaxLevel), "calm: collider of " + id + " (level " + e.level + ") " + (col != null && col.enabled ? "on" : "off"));
            }

            // ---- move: to the opposite corner
            viewer.position = new Vector3(W - span * 0.1f, 0, D - span * 0.1f);
            yield return Settle(st, report, "move");
            yield return Pass(st.unloadDelaySeconds + 0.3f);
            yield return Settle(st, report, "move, after unload delay");
            CheckCover(st, index, viewer, report, "move");
            CheckSkirts(st, report, "move", out _);
            if (nearOld != null && index.tilesX >= 4 && index.tilesY >= 4) report.Check(!st.IsLoaded(nearOld), "move: " + nearOld + ", near the old position, is still loaded");
            sb.Append(" move_unloads=").Append(st.Stats.unloads);

            // ---- rapid: a teleport every frame, then a back-and-forth every frame
            var rnd = new System.Random(7);
            int overlaps = 0;
            for (int i = 0; i < 90; i++)
            {
                viewer.position = new Vector3((float)rnd.NextDouble() * W, 0, (float)rnd.NextDouble() * D);
                yield return null;
                if (!OceanLodCover.IsDisjoint(st.Tree, Drawn(st))) overlaps++;
            }
            for (int i = 0; i < 60; i++)
            {
                viewer.position = i % 2 == 0 ? new Vector3(span * 0.2f, 0, span * 0.2f) : new Vector3(W - span * 0.2f, 0, D - span * 0.2f);
                yield return null;
                if (!OceanLodCover.IsDisjoint(st.Tree, Drawn(st))) overlaps++;
            }
            report.Check(overlaps == 0, "rapid: tiles overlapped in " + overlaps + " frames");
            report.Check(counting.maxInFlight <= st.maxInFlight, "rapid: " + counting.maxInFlight + " loads in flight, limit " + st.maxInFlight);
            report.Check(counting.maxLivePerId <= 1, "rapid: a tile was loaded twice at once (" + counting.maxLivePerId + ")");
            viewer.position = new Vector3(W * 0.3f, 0, D * 0.6f);
            yield return Settle(st, report, "after rapid");
            yield return Pass(st.unloadDelaySeconds + 0.3f);
            yield return Settle(st, report, "after rapid, stable");
            CheckCover(st, index, viewer, report, "after rapid");
            var s3 = st.Stats;
            // interrupted loads leave nothing behind: instances alive == tiles loaded
            int terrainsNow = FindObjectsByType<Terrain>(FindObjectsSortMode.None).Length - terrainsBefore;
            report.Check(counting.liveTotal == s3.loaded, "interrupt: " + counting.liveTotal + " tile loads held, " + s3.loaded + " tiles loaded (leak)");
            report.Check(terrainsNow == s3.loaded, "interrupt: " + terrainsNow + " terrains in the scene, " + s3.loaded + " loaded");
            sb.Append(" rapid_loads=").Append(s3.loadsStarted).Append(" cancelled=").Append(s3.loadsCancelled).Append(" max_in_flight=").Append(counting.maxInFlight);

            // ---- failure: a level-0 tile that cannot be loaded
            var victim = index.tiles[index.tiles.Length / 2];
            viewer.position = new Vector3(W + span * 10, 0, D + span * 10); // far away: the victim unloads
            yield return Pass(st.unloadDelaySeconds + 0.5f);
            counting.fail.Add(victim.id); failed.Clear();
            viewer.position = victim.position + new Vector3(victim.size.x / 2, 0, victim.size.z / 2);
            for (float end = Time.unscaledTime + 8f; failed.Count == 0 && Time.unscaledTime < end;) yield return null;
            report.Check(failed.Count > 0 && failed[0].id == victim.id, "failure: the failing tile " + victim.id + " was not reported");
            yield return Settle(st, report, "failure");
            var vnode = st.Tree.Get(victim.id);
            report.Check(!st.IsVisible(victim.id), "failure: the failed tile is drawn");
            if (vnode != null && vnode.parent != null)
            {
                bool covered = false; for (var p = vnode.parent; p != null; p = p.parent) covered |= st.IsVisible(p.Id);
                report.Check(covered, "failure: no ancestor of " + victim.id + " covers the ground");
            }
            CheckCover(st, index, viewer, report, "failure", false); // a kept parent covers more than a fresh selection would
            yield return Pass(3f); // the retries run out: retryAfter 0.3 s, doubling, two retries
            report.Check(failed.Count > 0 && failed[failed.Count - 1].final, "failure: the tile was not given up on after " + failed.Count + " failures");
            counting.fail.Clear(); st.Retry();
            yield return Settle(st, report, "retry");
            yield return Pass(0.2f);
            report.Check(st.IsVisible(victim.id), "failure: " + victim.id + " is not drawn after the failure was cleared");
            report.Check(counting.begins.TryGetValue(victim.id, out var nbeg) && nbeg >= 2, "failure: the tile was not retried");
            sb.Append(" failure_reports=").Append(failed.Count);

            // ---- release: destroying the streamer frees everything
            Destroy(go);
            yield return null; yield return null;
            report.Check(counting.liveTotal == 0, "release: " + counting.liveTotal + " tile loads still held after destroy");
            report.Check(FindObjectsByType<Terrain>(FindObjectsSortMode.None).Length == terrainsBefore, "release: " + (FindObjectsByType<Terrain>(FindObjectsSortMode.None).Length - terrainsBefore) + " terrains left after destroy");

            // ---- corrupt: an index whose entry points at an address that does not exist
            var bad = Instantiate(index);
            var badEntry = bad.tiles[bad.tiles.Length / 2];
            badEntry.address = "ocean/" + index.worldName + "/does-not-exist";
            var go2 = new GameObject("LodStreamerCorrupt");
            var st2 = go2.AddComponent<OceanLodStreamer>();
            var counting2 = new OceanCountingLoader(new OceanAddressablesTileLoader());
            st2.index = bad; st2.viewers.Add(viewer); st2.Loader = counting2; st2.lod.splitFactor = 0.6f; st2.maxInFlight = 3; st2.retryAfterSeconds = 0.2f; st2.maxRetries = 1; st2.updateIntervalSeconds = 0.02f;
            var failed2 = new List<string>(); st2.TileFailed += (e, m, f) => failed2.Add(e.id);
            viewer.position = badEntry.position + new Vector3(badEntry.size.x / 2, 0, badEntry.size.z / 2);
            for (float end = Time.unscaledTime + 8f; failed2.Count == 0 && Time.unscaledTime < end;) yield return null;
            report.Check(failed2.Contains(badEntry.id), "corrupt: a missing address was not reported");
            yield return Settle(st2, report, "corrupt");
            report.Check(st2.Stats.loaded > 0, "corrupt: nothing else was streamed");
            Destroy(go2); Destroy(bad);
            yield return null; yield return null;
            report.Check(counting2.liveTotal == 0, "corrupt: " + counting2.liveTotal + " loads still held");

            // ---- levels: an index without level 1, and one without any coarse level
            if (maxLevel >= 2)
            {
                var noL1 = Instantiate(index);
                var keep = new List<OceanTileEntry>(); foreach (var e in noL1.lodTiles) if (e.level != 1) keep.Add(e);
                noL1.lodTiles = keep.ToArray();
                yield return StreamAndCheck(noL1, viewer, new Vector3(span * 0.1f, 0, span * 0.1f), report, "no level 1");
                Destroy(noL1);
            }
            if (maxLevel >= 1)
            {
                var flat = Instantiate(index);
                flat.lodTiles = new OceanTileEntry[0]; flat.maxLevelImported = 0;
                yield return StreamAndCheck(flat, viewer, new Vector3(W * 0.5f, 0, D * 0.5f), report, "level 0 only");
                Destroy(flat);
            }

            // ---- seam: cracks counted by rendering
            if (SystemInfo.graphicsDeviceType != UnityEngine.Rendering.GraphicsDeviceType.Null)
            {
                Material skirtMat = null;
                var probeGo = new GameObject("LodSeamStreamer");
                var st3 = probeGo.AddComponent<OceanLodStreamer>();
                st3.index = index; st3.viewers.Add(viewer); st3.lod.splitFactor = 0.6f; st3.updateIntervalSeconds = 0.02f; st3.unloadDelaySeconds = 0.3f;
                viewer.position = new Vector3(span * 0.1f, 0, span * 0.1f);
                yield return Settle(st3, report, "seam world");
                foreach (var id in st3.VisibleIds) { var sk = st3.GetTerrain(id)?.GetComponent<OceanTileSkirts>(); if (sk != null && sk.east != null) { skirtMat = sk.east.sharedMaterial; break; } }
                yield return RealSeam(st3, index, report, sb);
                Destroy(probeGo);
                yield return null;
                yield return OceanSeamProbe.RunSynthetic(report, skirtMat, 0.5f); // 0.5 m: the importer's default skirt margin
            }
            else sb.Append(" seam=skipped(no graphics device)");

            if (hadRoot) sceneRoot.SetActive(true);
            if (hadSea) sea.SetActive(true);
            Destroy(viewer.gameObject);
            Debug.Log("OCEAN_PLAYER_LODSTREAM world=" + index.worldName + " levels=" + (maxLevel + 1) + " tiles=" + index.tiles.Length + "+" + index.lodTiles.Length + sb);
        }

        IEnumerator StreamAndCheck(OceanWorldIndex variant, Transform viewer, Vector3 at, OceanSmokeReport report, string tag)
        {
            var g = new GameObject("LodStreamer_" + tag);
            var s = g.AddComponent<OceanLodStreamer>();
            var c = new OceanCountingLoader(new OceanAddressablesTileLoader());
            s.index = variant; s.viewers.Add(viewer); s.Loader = c; s.lod.splitFactor = 0.6f; s.updateIntervalSeconds = 0.02f; s.unloadDelaySeconds = 0.3f;
            viewer.position = at;
            yield return Settle(s, report, tag);
            CheckCover(s, variant, viewer, report, tag);
            report.Check(s.Stats.loaded > 0, tag + ": nothing loaded");
            if (variant.maxLevelImported == 0) foreach (var id in s.VisibleIds) report.Check(variant.Get(id).level == 0, tag + ": " + id + " drawn although only level 0 exists");
            Destroy(g);
            yield return null; yield return null;
            report.Check(c.liveTotal == 0, tag + ": " + c.liveTotal + " loads still held after destroy");
        }

        // Background pixels seen near every drawn tile edge that borders another level, with the streamer's skirts and with all skirts off.
        IEnumerator RealSeam(OceanLodStreamer st, OceanWorldIndex index, OceanSmokeReport report, StringBuilder sb)
        {
            var camGo = new GameObject("LodSeamCam");
            var cam = camGo.AddComponent<Camera>();
            cam.clearFlags = CameraClearFlags.SolidColor; cam.backgroundColor = new Color(0, 1, 0); cam.fieldOfView = 40; cam.farClipPlane = 1e6f;
            var vis = new HashSet<OceanLodTree.Node>(Drawn(st));
            int on = 0, off = 0, views = 0;
            foreach (var n in vis)
            {
                if (views >= 24) break;
                var sides = OceanLodCover.SkirtSides(st.Tree, vis, n);
                if (sides == OceanSides.None) continue;
                var t = n.tile; float w = t.maxX - t.minX;
                var terr = st.GetTerrain(n.Id);
                for (int side = 0; side < 4 && views < 24; side++)
                {
                    if (((int)sides & (1 << side)) == 0) continue;
                    float ex = side == 1 ? t.maxX : side == 3 ? t.minX : (t.minX + t.maxX) / 2, ez = side == 0 ? t.maxZ : side == 2 ? t.minZ : (t.minZ + t.maxZ) / 2;
                    float ix = side == 1 ? -1 : side == 3 ? 1 : 0, iz = side == 0 ? -1 : side == 2 ? 1 : 0;
                    var edge = new Vector3(ex, 0, ez);
                    float ground = terr.SampleHeight(edge) + terr.transform.position.y;
                    cam.transform.position = edge + new Vector3(ix * w * 0.12f, 0, iz * w * 0.12f) + Vector3.up * (Mathf.Max(ground, 0) + w * 0.04f);
                    cam.transform.LookAt(edge + new Vector3(0, ground, 0));
                    yield return null;
                    int a = OceanSeamProbe.CountBackground(cam);
                    foreach (var o in vis) st.GetTerrain(o.Id)?.GetComponent<OceanTileSkirts>()?.Apply(OceanSides.None);
                    int b = OceanSeamProbe.CountBackground(cam);
                    foreach (var o in vis) st.GetTerrain(o.Id)?.GetComponent<OceanTileSkirts>()?.Apply(OceanLodCover.SkirtSides(st.Tree, vis, o));
                    on += a; off += b; views++;
                }
            }
            Destroy(camGo);
            sb.Append(" real_seam_views=").Append(views).Append(" background_px_with_skirts=").Append(on).Append(" without=").Append(off);
            report.Check(on <= off, "seam: real tiles show more cracks with skirts (" + on + ") than without (" + off + ")");
        }
    }
}
