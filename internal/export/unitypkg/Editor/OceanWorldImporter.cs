// Ocean Compiler Unity importer. Thin adapter: all processing happens in the Go core.
// Reads world.manifest.json, the 16-bit tile heightmaps, depth-class and shore masks and creates
//   Generated/Terrains/<id>.asset   TerrainData (heights + depth-class painted layers)
//   Generated/Prefabs/<id>.prefab   Terrain + TerrainCollider per tile, positioned in world space
//   Generated/OceanWorldIndex.asset tile index for runtime loading (ids, bounds, neighbours, addresses)
//   <world>.unity                   a scene with every tile and a translucent sea plane
// Addressables registration is done by OceanCompiler.World.Editor.Addressables when that package is installed.
//
// Batchmode:  Unity -batchmode -projectPath <p> -executeMethod OceanWorldImporter.ImportFromCommandLine -quit
// Screenshot (needs a GPU; do not pass -nographics):  ... -oceanShot <path.png>
// Import verifies orientation against manifest checkpoints and exits non-zero on mismatch.
using System;
using System.Collections.Generic;
using System.IO;
using OceanCompiler.World;
using UnityEditor;
using UnityEngine;

public static class OceanWorldImporter
{
    [Serializable] class Grid { public int tile_cells; public int samples_per_tile; public float tile_span_m; public int tiles_x; public int tiles_y; public float cell_m; public double origin_easting; public double origin_northing_top; }
    [Serializable] class Height { public float min_m; public float max_m; public string row_order; }
    [Serializable] class Crs { public int epsg; }
    [Serializable] class DepthClass { public int id; }
    [Serializable] class TileInfo
    {
        public string id; public int level; public int col; public int row;
        public double[] bounds_utm; public float min_m; public float max_m;
        public float nodata_fraction; public float water_fraction; public float measured_water_fraction;
        public string height; public string depth_class; public string shore_mask;
    }
    [Serializable] class Checkpoint { public string name; public double easting; public double northing; public double elevation_m; }
    [Serializable]
    class Manifest
    {
        public string name; public string build_id; public Crs crs; public Grid grid; public Height height_encoding;
        public DepthClass[] depth_classes; public TileInfo[] tiles; public Checkpoint[] checkpoints; public string[] attribution;
    }

    /// <summary>Raised after a world is imported: index, tile id to prefab asset path. Used by the Addressables module.</summary>
    public static event Action<OceanWorldIndex, Dictionary<string, string>> TilesImported;

    const double Tolerance = 0.05; // metres; 16-bit quantisation over typical ranges is ~0.005 m
    // URP and HDRP terrain shaders draw 4 layers per pass; a 5th needs an extra pass shader that did not render (magenta) in the URP test, so 4 layers.
    const int Layers = 4; // land, shore, shallow, deep
    static readonly Color[] LayerColors =
    {
        new Color(0.33f, 0.45f, 0.24f), new Color(0.76f, 0.70f, 0.50f), new Color(0.45f, 0.65f, 0.60f), new Color(0.10f, 0.20f, 0.34f),
    };
    static readonly string[] LayerNames = { "land", "shore", "shallow", "deep" };

    [MenuItem("Ocean/Import World")]
    public static void ImportMenu()
    {
        foreach (var dir in Directory.GetDirectories("Assets/OceanWorld")) Import(dir.Replace('\\', '/'));
    }

    public static void ImportFromCommandLine()
    {
        try
        {
            ImportMenu();
            var args = Environment.GetCommandLineArgs();
            int i = Array.IndexOf(args, "-oceanShot");
            if (i >= 0 && i + 1 < args.Length) Screenshot(args[i + 1]);
        }
        catch (Exception e) { Debug.LogError("Ocean import failed: " + e); EditorApplication.Exit(1); }
    }

    static Shader FindShader(params string[] names)
    {
        foreach (var n in names) { var s = Shader.Find(n); if (s != null) return s; }
        throw new Exception("none of the shaders found: " + string.Join(", ", names));
    }

    static Material TransparentMaterial(Color c)
    {
        var shader = FindShader("Universal Render Pipeline/Lit", "HDRP/Lit", "Standard");
        var mat = new Material(shader);
        mat.color = c;
        if (mat.HasProperty("_BaseColor")) mat.SetColor("_BaseColor", c);
        if (shader.name.StartsWith("Universal"))
        {
            mat.SetFloat("_Surface", 1); mat.SetFloat("_Blend", 0);
            mat.SetFloat("_SrcBlend", (float)UnityEngine.Rendering.BlendMode.SrcAlpha);
            mat.SetFloat("_DstBlend", (float)UnityEngine.Rendering.BlendMode.OneMinusSrcAlpha);
            mat.SetFloat("_ZWrite", 0);
            mat.EnableKeyword("_SURFACE_TYPE_TRANSPARENT");
            mat.SetOverrideTag("RenderType", "Transparent");
        }
        else if (shader.name == "Standard")
        {
            mat.SetFloat("_Mode", 2);
            mat.SetInt("_SrcBlend", (int)UnityEngine.Rendering.BlendMode.SrcAlpha);
            mat.SetInt("_DstBlend", (int)UnityEngine.Rendering.BlendMode.OneMinusSrcAlpha);
            mat.SetInt("_ZWrite", 0);
            mat.EnableKeyword("_ALPHABLEND_ON");
        }
        mat.renderQueue = 3000;
        return mat;
    }

    static TerrainLayer[] MakeLayers(string gen)
    {
        var layers = new TerrainLayer[Layers];
        for (int k = 0; k < Layers; k++)
        {
            string texPath = gen + "/Layers/" + LayerNames[k] + ".asset", layerPath = gen + "/Layers/" + LayerNames[k] + ".terrainlayer";
            var tex = AssetDatabase.LoadAssetAtPath<Texture2D>(texPath);
            if (tex == null)
            {
                tex = new Texture2D(4, 4); var px = new Color[16];
                for (int p = 0; p < 16; p++) px[p] = LayerColors[k];
                tex.SetPixels(px); tex.Apply();
                AssetDatabase.CreateAsset(tex, texPath);
            }
            layers[k] = AssetDatabase.LoadAssetAtPath<TerrainLayer>(layerPath);
            if (layers[k] == null)
            {
                layers[k] = new TerrainLayer { diffuseTexture = tex, tileSize = new Vector2(100, 100) };
                AssetDatabase.CreateAsset(layers[k], layerPath);
            }
        }
        return layers;
    }

    // 8-bit grey PNG read back exactly; returned as [z * S + x] with z growing north.
    static byte[] ReadMask(string path, int S)
    {
        var tex = new Texture2D(2, 2, TextureFormat.RGBA32, false, true);
        if (!tex.LoadImage(File.ReadAllBytes(path)) || tex.width != S || tex.height != S) throw new Exception(path + ": expected " + S + "x" + S + " PNG");
        var px = tex.GetPixels32();
        UnityEngine.Object.DestroyImmediate(tex);
        var o = new byte[S * S];
        for (int i = 0; i < o.Length; i++) o[i] = px[i].r; // LoadImage puts the PNG's first row at the top (y = S-1), i.e. north
        return o;
    }

    // Layer weights for one tile: shore sand on shore-mask cells, otherwise land or a depth band.
    static float[,,] Paint(byte[] cls, byte[] shore, int S, int res, int waterClasses)
    {
        var a = new float[res, res, Layers];
        for (int z = 0; z < res; z++)
            for (int x = 0; x < res; x++)
            {
                int vz = Math.Min(S - 1, (int)Math.Round((z + 0.5) * (S - 1) / res)), vx = Math.Min(S - 1, (int)Math.Round((x + 0.5) * (S - 1) / res));
                int c = cls[vz * S + vx], layer;
                if (c == 0) layer = shore[vz * S + vx] != 0 ? 1 : 0;
                else if (c == 255) layer = 3; // nodata: drawn as deep, not hidden
                else
                {
                    double f = waterClasses <= 1 ? 1 : (c - 1.0) / (waterClasses - 1);
                    layer = f < 0.4 ? 2 : 3; // roughly the shallow half of the depth bands vs the rest
                }
                a[z, x, layer] = 1;
            }
        return a;
    }

    public static void Import(string dir)
    {
        var m = JsonUtility.FromJson<Manifest>(File.ReadAllText(Path.Combine(dir, "world.manifest.json")));
        if (m.height_encoding.row_order != "north_to_south") throw new Exception("unsupported row_order " + m.height_encoding.row_order);
        int S = m.grid.samples_per_tile, res = Math.Max(16, Math.Min(2048, m.grid.tile_cells));
        float span = m.grid.tile_span_m, range = m.height_encoding.max_m - m.height_encoding.min_m;
        int waterClasses = 0;
        foreach (var d in m.depth_classes) if (d.id > 0 && d.id != 255) waterClasses++;

        // Assets are updated in place, never deleted and recreated: their GUIDs (and so Addressables
        // AssetReferences, scene links and VCS history) stay stable across re-imports of the same tile ids.
        string gen = dir + "/Generated";
        if (!AssetDatabase.IsValidFolder(gen)) AssetDatabase.CreateFolder(dir, "Generated");
        foreach (var f in new[] { "Terrains", "Prefabs", "Layers" }) if (!AssetDatabase.IsValidFolder(gen + "/" + f)) AssetDatabase.CreateFolder(gen, f);

        var layers = MakeLayers(gen);
        // No terrain material template: the active render pipeline's default terrain material is used, so the
        // same TerrainData renders under Built-in, URP and HDRP. (A hand-made URP Terrain/Lit template rendered magenta beyond basemapDistance.)
        var index = ScriptableObject.CreateInstance<OceanWorldIndex>();
        index.worldName = m.name; index.buildId = m.build_id; index.epsg = m.crs.epsg;
        index.originEasting = m.grid.origin_easting;
        index.originNorthingSouth = m.grid.origin_northing_top - m.grid.tiles_y * (double)span;
        index.cellM = m.grid.cell_m; index.tileSpanM = span; index.tilesX = m.grid.tiles_x; index.tilesY = m.grid.tiles_y;
        index.minM = m.height_encoding.min_m; index.maxM = m.height_encoding.max_m; index.attribution = m.attribution;
        var byCell = new Dictionary<(int, int), TileInfo>();
        foreach (var t in m.tiles) byCell[(t.col, t.row)] = t;
        string Id(int c, int r) => byCell.TryGetValue((c, r), out var t) ? t.id : "";
        var entries = new List<OceanTileEntry>();
        var prefabPaths = new Dictionary<string, string>();

        foreach (var t in m.tiles)
        {
            byte[] raw = File.ReadAllBytes(Path.Combine(dir, t.height));
            if (raw.Length != S * S * 2) throw new Exception(t.id + ": expected " + S * S * 2 + " bytes, got " + raw.Length);
            var h = new float[S, S]; // h[z, x], z grows north; file rows run north to south
            for (int i = 0; i < S; i++)
                for (int j = 0; j < S; j++)
                    h[S - 1 - i, j] = (raw[(i * S + j) * 2] | (raw[(i * S + j) * 2 + 1] << 8)) / 65535f;

            string tdPath = gen + "/Terrains/" + t.id + ".asset";
            var td = AssetDatabase.LoadAssetAtPath<TerrainData>(tdPath);
            bool newTd = td == null;
            if (newTd) td = new TerrainData();
            td.heightmapResolution = S; td.size = new Vector3(span, range, span);
            td.SetHeights(0, 0, h);
            td.terrainLayers = layers;
            td.alphamapResolution = res;
            td.SetAlphamaps(0, 0, Paint(ReadMask(Path.Combine(dir, t.depth_class), S), ReadMask(Path.Combine(dir, t.shore_mask), S), S, res, waterClasses));
            if (newTd) AssetDatabase.CreateAsset(td, tdPath); else EditorUtility.SetDirty(td);

            // Tile row 0 is the northernmost; world +Z is north, y = 0 is sea level.
            var pos = new Vector3(t.col * span, m.height_encoding.min_m, (m.grid.tiles_y - 1 - t.row) * span);
            var go = Terrain.CreateTerrainGameObject(td);
            go.name = t.id;
            go.transform.position = pos;
            string prefab = gen + "/Prefabs/" + t.id + ".prefab";
            PrefabUtility.SaveAsPrefabAsset(go, prefab);
            UnityEngine.Object.DestroyImmediate(go);
            prefabPaths[t.id] = prefab;

            entries.Add(new OceanTileEntry
            {
                id = t.id, level = t.level, col = t.col, row = t.row, position = pos, size = new Vector3(span, range, span),
                minM = t.min_m, maxM = t.max_m, minEasting = t.bounds_utm[0], minNorthing = t.bounds_utm[1], maxEasting = t.bounds_utm[2], maxNorthing = t.bounds_utm[3],
                waterFraction = t.water_fraction, measuredWaterFraction = t.measured_water_fraction, nodataFraction = t.nodata_fraction,
                address = "ocean/" + m.name + "/" + t.id,
                west = Id(t.col - 1, t.row), east = Id(t.col + 1, t.row), north = Id(t.col, t.row - 1), south = Id(t.col, t.row + 1),
            });
        }
        index.tiles = entries.ToArray();
        var cps = new List<OceanCheckpoint>();
        foreach (var c in m.checkpoints) cps.Add(new OceanCheckpoint { name = c.name, easting = c.easting, northing = c.northing, elevationM = (float)c.elevation_m });
        index.checkpoints = cps.ToArray();
        string indexPath = gen + "/OceanWorldIndex.asset";
        var oldIndex = AssetDatabase.LoadAssetAtPath<OceanWorldIndex>(indexPath);
        if (oldIndex == null) AssetDatabase.CreateAsset(index, indexPath);
        else { EditorUtility.CopySerialized(index, oldIndex); UnityEngine.Object.DestroyImmediate(index); EditorUtility.SetDirty(oldIndex); }
        // Tiles that left the manifest (bbox shrank) are removed; the rest keep their assets.
        var keep = new HashSet<string>(prefabPaths.Keys);
        foreach (var sub in new[] { "Terrains", "Prefabs" })
            foreach (var guid in AssetDatabase.FindAssets("", new[] { gen + "/" + sub }))
            {
                string path = AssetDatabase.GUIDToAssetPath(guid);
                if (!keep.Contains(Path.GetFileNameWithoutExtension(path))) AssetDatabase.DeleteAsset(path);
            }

        // Scene: every tile prefab plus a sea plane; neighbours linked through the same runtime helper games use.
        var scene = UnityEditor.SceneManagement.EditorSceneManager.NewScene(UnityEditor.SceneManagement.NewSceneSetup.DefaultGameObjects, UnityEditor.SceneManagement.NewSceneMode.Single);
        // A new scene unloads unreferenced objects; continue with the saved asset, not the in-memory instance.
        index = AssetDatabase.LoadAssetAtPath<OceanWorldIndex>(indexPath);
        var root = new GameObject("Ocean_" + m.name);
        var loaded = new Dictionary<string, Terrain>();
        foreach (var e in index.tiles)
        {
            var inst = (GameObject)PrefabUtility.InstantiatePrefab(AssetDatabase.LoadAssetAtPath<GameObject>(prefabPaths[e.id]), root.transform);
            loaded[e.id] = inst.GetComponent<Terrain>();
        }
        foreach (var e in index.tiles) index.LinkNeighbours(e, loaded[e.id], id => loaded.TryGetValue(id, out var t) ? t : null);

        var sea = GameObject.CreatePrimitive(PrimitiveType.Plane);
        sea.name = "SeaLevel";
        sea.transform.SetParent(root.transform);
        float width = m.grid.tiles_x * span, depth = m.grid.tiles_y * span;
        sea.transform.position = new Vector3(width / 2, 0, depth / 2);
        sea.transform.localScale = new Vector3(width / 10, 1, depth / 10);
        var mat = AssetDatabase.LoadAssetAtPath<Material>(gen + "/Sea.mat");
        if (mat == null) { mat = TransparentMaterial(new Color(0.05f, 0.3f, 0.5f, 0.45f)); AssetDatabase.CreateAsset(mat, gen + "/Sea.mat"); }
        sea.GetComponent<Renderer>().sharedMaterial = mat;

        double maxErr = Verify(m, loaded, index, span);
        var scenePath = dir + "/" + m.name + ".unity";
        UnityEditor.SceneManagement.EditorSceneManager.SaveScene(scene, scenePath);
        EditorUtility.SetDirty(index);
        AssetDatabase.SaveAssets();
        TilesImported?.Invoke(index, prefabPaths);
        AssetDatabase.SaveAssets();
        Debug.Log("OCEAN_VERIFY checkpoints=" + m.checkpoints.Length + " max_err=" + maxErr.ToString("F4") + " m");
        Debug.Log("OCEAN_IMPORT_OK " + m.name + " tiles=" + m.tiles.Length + " scene=" + scenePath + " index=" + gen + "/OceanWorldIndex.asset");
    }

    // Convert each manifest checkpoint (UTM) to Unity space independently of tile placement
    // and compare the terrain height there with the expected elevation.
    static double Verify(Manifest m, Dictionary<string, Terrain> loaded, OceanWorldIndex index, float span)
    {
        if (m.checkpoints == null || m.checkpoints.Length == 0) throw new Exception("manifest has no checkpoints");
        double maxErr = 0;
        foreach (var c in m.checkpoints)
        {
            var world = index.UtmToUnity(c.easting, c.northing);
            var tile = index.TileAt(world);
            if (tile == null) throw new Exception("checkpoint " + c.name + " is outside every tile");
            var terrain = loaded[tile.id];
            double y = terrain.SampleHeight(world) + terrain.transform.position.y;
            double err = Math.Abs(y - c.elevation_m);
            maxErr = Math.Max(maxErr, err);
            Debug.Log("OCEAN_CHECK " + c.name + " tile=" + tile.id + " expected=" + c.elevation_m.ToString("F3") + " got=" + y.ToString("F3"));
            if (err > Tolerance) throw new Exception("checkpoint " + c.name + " expected " + c.elevation_m + " m, terrain gives " + y + " m: world is misplaced or flipped");
        }
        return maxErr;
    }

    static void Screenshot(string path)
    {
        // Shaders compile asynchronously in the editor and render as the pink error shader until ready.
        EditorSettings.asyncShaderCompilation = false;
        var terrains = UnityEngine.Object.FindObjectsByType<Terrain>(FindObjectsSortMode.None);
        // Batchmode never ticks the editor, so URP's far-LOD terrain base map is never generated and distant terrain renders
        // magenta in this screenshot. Draw full-quality layers instead (in-memory only; play mode and players are unaffected, see the PlayMode test).
        foreach (var t in terrains) t.basemapDistance = 1000000;
        var b = new Bounds(terrains[0].transform.position, Vector3.zero);
        foreach (var t in terrains) b.Encapsulate(new Bounds(t.transform.position + t.terrainData.size / 2, t.terrainData.size));
        var camGo = new GameObject("ShotCam");
        var cam = camGo.AddComponent<Camera>();
        cam.fieldOfView = 45; cam.farClipPlane = 200000; cam.clearFlags = CameraClearFlags.SolidColor;
        cam.backgroundColor = new Color(0.62f, 0.75f, 0.9f);
        // Look from the south-east, north is up-screen ahead; the world is shown from above.
        cam.transform.position = b.center + new Vector3(b.size.x * 0.35f, b.size.x * 0.75f, -b.size.z * 0.6f);
        cam.transform.LookAt(b.center + new Vector3(0, -b.size.y * 0.5f, b.size.z * 0.05f));
        var light = UnityEngine.Object.FindFirstObjectByType<Light>();
        if (light != null) { light.transform.rotation = Quaternion.Euler(40, 300, 0); light.intensity = 1.3f; }
        var rt = new RenderTexture(1920, 1080, 24);
        cam.targetTexture = rt;
        var pipe = UnityEngine.Rendering.GraphicsSettings.currentRenderPipeline;
        Debug.Log("OCEAN_PIPELINE " + (pipe != null ? pipe.GetType().Name : "Built-in"));
        // First render triggers shader compilation; wait for it, then render again for the image.
        cam.Render();
        for (int i = 0; i < 600 && ShaderUtil.anythingCompiling; i++) System.Threading.Thread.Sleep(100);
        cam.Render();
        RenderTexture.active = rt;
        var img = new Texture2D(rt.width, rt.height, TextureFormat.RGB24, false);
        img.ReadPixels(new Rect(0, 0, rt.width, rt.height), 0, 0); img.Apply();
        Directory.CreateDirectory(Path.GetDirectoryName(Path.GetFullPath(path)));
        File.WriteAllBytes(path, img.EncodeToPNG());
        RenderTexture.active = null;
        Debug.Log("OCEAN_SHOT " + path);
    }
}
