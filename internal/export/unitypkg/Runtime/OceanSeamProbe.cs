// Measures cracks between terrain tiles of different sizes by rendering: the camera clears to pure green and the probe counts
// green pixels, so a crack through which the background shows is a number. RunSynthetic builds a fine tile pair beside a tile of
// twice the size and compares three cases: all tiles fine (the reference, no seam), mixed sizes without skirts, mixed with skirts.
// Needs a graphics device (not -nographics). Used by the PlayMode tests and the player smoke.
using System.Collections;
using System.Text;
using UnityEngine;

namespace OceanCompiler.World
{
    public static class OceanSeamProbe
    {
        /// <summary>Renders <paramref name="cam"/> once and returns the number of pure-green pixels (the camera's clear colour).</summary>
        public static int CountBackground(Camera cam, int width = 960, int height = 540)
        {
            var rt = RenderTexture.GetTemporary(width, height, 24);
            var old = RenderTexture.active; var oldTarget = cam.targetTexture;
            cam.targetTexture = rt; cam.Render();
            RenderTexture.active = rt;
            var img = new Texture2D(width, height, TextureFormat.RGB24, false);
            img.ReadPixels(new Rect(0, 0, width, height), 0, 0); img.Apply();
            RenderTexture.active = old; cam.targetTexture = oldTarget; RenderTexture.ReleaseTemporary(rt);
            int n = 0;
            foreach (var c in img.GetPixels32()) if (c.g > 200 && c.r < 40 && c.b < 40) n++;
            Object.Destroy(img);
            return n;
        }

        const int S = 257; // samples per tile: 256 cells
        static float F(float x, float z) => 10f * Mathf.Sin(x * 0.05f + z * 0.03f) + 6f * Mathf.Sin(z * 0.11f + 0.3f) + 1.2f * Mathf.Sin(x * 0.35f - z * 0.3f);

        static Terrain MakeTile(Transform root, string name, Vector3 pos, float span, System.Func<int, int, float> height, float baseY, float range)
        {
            var td = new TerrainData { heightmapResolution = S, size = new Vector3(span, range, span) };
            var a = new float[S, S];
            for (int z = 0; z < S; z++) for (int x = 0; x < S; x++) a[z, x] = Mathf.Clamp01((height(x, z) - baseY) / range);
            td.SetHeights(0, 0, a);
            var tex = new Texture2D(4, 4); var px = new Color[16]; for (int i = 0; i < 16; i++) px[i] = new Color(0.5f, 0.5f, 0.5f); tex.SetPixels(px); tex.Apply();
            td.terrainLayers = new[] { new TerrainLayer { diffuseTexture = tex, tileSize = new Vector2(50, 50) } };
            var go = Terrain.CreateTerrainGameObject(td);
            go.name = name; go.transform.SetParent(root, false); go.transform.position = pos + new Vector3(0, baseY, 0);
            var t = go.GetComponent<Terrain>(); t.basemapDistance = 1000000;
            return t;
        }

        /// <summary>
        /// Synthetic seam test (heights are a smooth function sampled at 1 m and, for the coarse tile, at 2 m, as vertex decimation does).
        /// Reports the green pixel counts of 12 low views along the seam. Passes when the reference shows none, the unskirted seam shows
        /// some (so the test can see cracks at all) and the skirted seam shows none.
        /// </summary>
        public static IEnumerator RunSynthetic(OceanSmokeReport report, Material skirtMaterial, float skirtUpM)
        {
            if (SystemInfo.graphicsDeviceType == UnityEngine.Rendering.GraphicsDeviceType.Null) { Debug.Log("OCEAN_SEAM_SYNTHETIC skipped: no graphics device"); yield break; }
            const float baseY = -50f, range = 100f;
            Debug.Log("OCEAN_SEAM_MATERIAL " + (skirtMaterial == null ? "null" : skirtMaterial.name + " shader=" + (skirtMaterial.shader == null ? "null" : skirtMaterial.shader.name + " supported=" + skirtMaterial.shader.isSupported) + " pipeline=" + (UnityEngine.Rendering.GraphicsSettings.currentRenderPipeline != null)));
            int refPx = 0, rawPx = 0, skirtPx = 0;
            foreach (var variant in new[] { "ref", "raw", "skirt" })
            {
                var root = new GameObject("SeamSynthetic_" + variant).transform;
                Terrain A, A2, B = null;
                if (variant == "ref")
                {
                    var g = new Terrain[3, 2];
                    for (int i = 0; i < 3; i++) for (int j = 0; j < 2; j++) { int ox = i * 256, oz = j * 256; g[i, j] = MakeTile(root, "R" + i + j, new Vector3(ox, 0, oz), 256, (x, z) => F(ox + x, oz + z), baseY, range); }
                    for (int i = 0; i < 3; i++) for (int j = 0; j < 2; j++) g[i, j].SetNeighbors(i > 0 ? g[i - 1, j] : null, j < 1 ? g[i, j + 1] : null, i < 2 ? g[i + 1, j] : null, j > 0 ? g[i, j - 1] : null);
                    A = g[0, 0]; A2 = g[0, 1];
                }
                else
                {
                    A = MakeTile(root, "A", new Vector3(0, 0, 0), 256, (x, z) => F(x, z), baseY, range);
                    A2 = MakeTile(root, "A2", new Vector3(0, 0, 256), 256, (x, z) => F(x, 256 + z), baseY, range);
                    B = MakeTile(root, "B", new Vector3(256, 0, 0), 512, (x, z) => F(256 + 2 * x, 2 * z), baseY, range);
                    A.SetNeighbors(null, A2, null, null); A2.SetNeighbors(null, null, null, A); // B is another size: Unity cannot stitch it
                    if (variant == "skirt" && skirtMaterial != null)
                        foreach (var (t, side) in new[] { (A, OceanSides.East), (A2, OceanSides.East), (B, OceanSides.West) })
                        {
                            var td = t.terrainData; var h = td.GetHeights(0, 0, S, S);
                            OceanSkirtBuilder.Attach(t.gameObject, OceanSkirtBuilder.BuildMeshes(h, S, td.size.x / (S - 1), td.size.y, skirtUpM), skirtMaterial).Apply(side);
                        }
                }
                var camGo = new GameObject("SeamCam"); camGo.transform.SetParent(root, false);
                var cam = camGo.AddComponent<Camera>();
                cam.clearFlags = CameraClearFlags.SolidColor; cam.backgroundColor = new Color(0, 1, 0); cam.fieldOfView = 30; cam.farClipPlane = 3000;
                yield return null; // let the terrains and (URP) shaders come up
                int total = 0;
                foreach (float z in new[] { 40f, 100f, 160f, 220f, 300f, 400f })
                    foreach (int side in new[] { -1, 1 })
                    {
                        cam.transform.position = new Vector3(256 + side * 40f, F(256, z) + 25f, z - 5f);
                        cam.transform.LookAt(new Vector3(256, F(256, z), z + 15f));
                        CountBackground(cam); // first render compiles shaders
                        yield return null;
                        total += CountBackground(cam);
                    }
                if (variant == "ref") refPx = total; else if (variant == "raw") rawPx = total; else skirtPx = total;
                Object.Destroy(root.gameObject);
                yield return null;
            }
            Debug.Log("OCEAN_SEAM_SYNTHETIC reference=" + refPx + " mixed_no_skirt=" + rawPx + " mixed_skirt=" + skirtPx + " skirt_up_m=" + skirtUpM);
            report.Check(refPx == 0, "seam synthetic: the all-fine reference shows " + refPx + " background pixels");
            report.Check(rawPx > 200, "seam synthetic: the unskirted mixed seam shows only " + rawPx + " background pixels (the probe cannot see cracks)");
            if (skirtMaterial != null) report.Check(skirtPx == 0, "seam synthetic: skirted seam still shows " + skirtPx + " background pixels");
        }
    }
}
