// Ocean Compiler Unity importer. Thin adapter: all processing happens in the Go core.
// Reads world.manifest.json, the 16-bit tile heightmaps, depth-class and shore masks and creates
//   Generated/Terrains/<id>.asset   TerrainData (heights + depth-class painted layers)
//   Generated/Prefabs/<id>.prefab   Terrain + TerrainCollider per tile, positioned in world space
//   Generated/OceanWorldIndex.asset tile index for runtime loading (ids, bounds, neighbours, addresses)
//   <world>.unity                   a scene with every tile and a translucent sea plane
// Addressables registration is done by OceanCompiler.World.Editor.Addressables when that package is installed.
//
// Batchmode:  Unity -batchmode -projectPath <p> -executeMethod OceanWorldImporter.ImportFromCommandLine [-oceanMaxLevel N] -quit
// Screenshot (needs a GPU; do not pass -nographics):  ... -oceanShot <path.png> [-oceanHideSea]
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
    [Serializable] class Height { public string type; public float nodata; public float min_m; public float max_m; public string row_order; }
    [Serializable] class Crs { public int epsg; }
    [Serializable] class DepthClass { public int id; }
    [Serializable] class TileInfo
    {
        public string id; public int level; public int col; public int row;
        public double[] bounds_utm; public float min_m; public float max_m;
        public float nodata_fraction; public float water_fraction; public float measured_water_fraction;
        public string height; public string depth_class; public string shore_mask;
        public TileHash sha256;
    }
    [Serializable] class TileHash { public string height; }
    [Serializable] class LodLevel { public int level; }
    [Serializable] class LodInfo { public string method; public LodLevel[] levels; }
    [Serializable] class SmProps { public string osm_type; public long osm_id; public string seamark_type; public string name; public double easting; public double northing; public double dem_elevation_m; }
    [Serializable] class SmFeature { public SmProps properties; }
    [Serializable] class SmFile { public SmFeature[] features; }
    [Serializable] class Checkpoint { public string name; public double easting; public double northing; public double elevation_m; }
    [Serializable]
    class Manifest
    {
        public string name; public string build_id; public Crs crs; public Grid grid; public Height height_encoding;
        public DepthClass[] depth_classes; public TileInfo[] tiles; public LodInfo lod; public Checkpoint[] checkpoints; public string[] attribution;
    }

    /// <summary>Raised after a world is imported: index, tile id to prefab asset path. Used by the Addressables module.</summary>
    public static event Action<OceanWorldIndex, Dictionary<string, string>> TilesImported;

    const double Tolerance = 0.05; // metres, the floor. Unity stores 16-bit heights: one step is range/65535 (0.09 m for a 5.9 km range), so the check allows one step.
    const int Layers = 5; // land, shore, shallow, mid, deep
    static readonly Color[] LayerColors =
    {
        new Color(0.33f, 0.45f, 0.24f), new Color(0.76f, 0.70f, 0.50f), new Color(0.50f, 0.68f, 0.62f),
        new Color(0.22f, 0.42f, 0.50f), new Color(0.08f, 0.16f, 0.30f),
    };
    static readonly string[] LayerNames = { "land", "shore", "shallow", "mid", "deep" };

    [MenuItem("Ocean/Import World")]
    public static void ImportMenu()
    {
        if (!(UnityEngine.Rendering.GraphicsSettings.currentRenderPipeline is UnityEngine.Rendering.Universal.UniversalRenderPipelineAsset))
            throw new Exception("Ocean importer supports the Universal Render Pipeline only: assign a URP asset in Project Settings > Graphics.");
        foreach (var dir in Directory.GetDirectories("Assets/OceanWorld")) Import(dir.Replace('\\', '/'));
    }

    /// <summary>Highest LOD level to import as tile prefabs (0 = only level 0, the default). Coarser levels go to
    /// OceanWorldIndex.lodTiles and the Addressables groups; the scene and the streamer keep using level 0.</summary>
    public static int MaxLevel = 0;

    /// <summary>Skirts (crack hiding strips on tile edges) are built when coarser levels are imported. They reach this many metres above the edge samples
    /// (batch: -oceanSkirtUp). 0.5 m closed every crack in the seam experiment (docs/unity-lod.md); raise it if cracks show at your view distances.</summary>
    public static float SkirtUpM = 0.5f;

    public static void ImportFromCommandLine()
    {
        try
        {
            var cl = Environment.GetCommandLineArgs();
            int ml = Array.IndexOf(cl, "-oceanMaxLevel");
            if (ml >= 0 && ml + 1 < cl.Length) MaxLevel = int.Parse(cl[ml + 1]);
            int su = Array.IndexOf(cl, "-oceanSkirtUp");
            if (su >= 0 && su + 1 < cl.Length) SkirtUpM = float.Parse(cl[su + 1], System.Globalization.CultureInfo.InvariantCulture);
            ImportMenu();
            var args = Environment.GetCommandLineArgs();
            int i = Array.IndexOf(args, "-oceanShot");
            if (i >= 0 && i + 1 < args.Length) Screenshot(args[i + 1]);
        }
        catch (Exception e) { Debug.LogError("Ocean import failed: " + e); EditorApplication.Exit(1); }
    }

    static Material TransparentMaterial(Color c)
    {
        var shader = Shader.Find("Universal Render Pipeline/Lit") ?? throw new Exception("URP Lit shader not found: this package supports URP only");
        var mat = new Material(shader);
        mat.SetColor("_BaseColor", c);
        mat.SetFloat("_Surface", 1); mat.SetFloat("_Blend", 0);
        mat.SetFloat("_SrcBlend", (float)UnityEngine.Rendering.BlendMode.SrcAlpha);
        mat.SetFloat("_DstBlend", (float)UnityEngine.Rendering.BlendMode.OneMinusSrcAlpha);
        mat.SetFloat("_ZWrite", 0);
        mat.EnableKeyword("_SURFACE_TYPE_TRANSPARENT");
        mat.SetOverrideTag("RenderType", "Transparent");
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

    static Material SkirtMaterial(string gen)
    {
        var shader = Shader.Find("Universal Render Pipeline/Lit") ?? throw new Exception("URP Lit shader not found: this package supports URP only");
        var fresh = new Material(shader);
        fresh.SetColor("_BaseColor", LayerColors[2]); // shallow-water tone: the strip is seen only where a crack would be
        fresh.SetFloat("_Cull", 0); // both faces: a crack is seen from either side
        fresh.SetFloat("_Smoothness", 0f);
        var mat = AssetDatabase.LoadAssetAtPath<Material>(gen + "/Skirt.mat");
        if (mat == null) { AssetDatabase.CreateAsset(fresh, gen + "/Skirt.mat"); return fresh; }
        EditorUtility.CopySerialized(fresh, mat); EditorUtility.SetDirty(mat);
        UnityEngine.Object.DestroyImmediate(fresh);
        return mat;
    }

    /// <summary>
    /// Builds the tile's four skirts (OceanSkirtBuilder) and adds them to the terrain object, with the meshes saved as one asset file.
    /// h is the tile's heightmap as imported (h[z, x], 0..1 of range, z grows north).
    /// </summary>
    static void AddSkirts(GameObject terrainGo, float[,] h, int S, float cell, float range, float up, Material mat, string meshAssetPath)
    {
        var meshes = OceanSkirtBuilder.BuildMeshes(h, S, cell, range, up);
        // one asset file holds the four meshes; it is recreated on every import (the prefab that references them is re-saved too)
        if (AssetDatabase.LoadMainAssetAtPath(meshAssetPath) != null) AssetDatabase.DeleteAsset(meshAssetPath);
        AssetDatabase.CreateAsset(meshes[0], meshAssetPath);
        for (int side = 1; side < 4; side++) AssetDatabase.AddObjectToAsset(meshes[side], meshAssetPath);
        OceanSkirtBuilder.Attach(terrainGo, meshes, mat);
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
                else if (c == 255) layer = 4; // nodata: drawn as deep, not hidden
                else
                {
                    double f = waterClasses <= 1 ? 1 : (c - 1.0) / (waterClasses - 1);
                    layer = f < 0.2 ? 2 : f < 0.6 ? 3 : 4;
                }
                a[z, x, layer] = 1;
            }
        return a;
    }

    /// <summary>
    /// Checks every file the import will read before any asset is touched: the requested level exists in the world,
    /// each tile's files exist, the height file has the right size and the sha256 the manifest recorded. A world that
    /// was exported without the LOD tiles (ocean export -level N), truncated or edited fails here with the tile named.
    /// </summary>
    static void VerifyFiles(string dir, Manifest m, int maxLevel, int S)
    {
        int levels = m.lod != null && m.lod.levels != null ? m.lod.levels.Length : 1;
        if (maxLevel < 0 || maxLevel >= levels)
            throw new Exception("-oceanMaxLevel " + maxLevel + " is not available: the world has LOD levels 0.." + (levels - 1));
        using (var sha = System.Security.Cryptography.SHA256.Create())
            foreach (var t in m.tiles)
            {
                if (t.level > maxLevel) continue;
                foreach (var f in new[] { t.height, t.depth_class, t.shore_mask })
                    if (!File.Exists(Path.Combine(dir, f)))
                        throw new Exception(t.id + ": file " + f + " is missing from the project (export with `ocean export -level " + maxLevel + "`)");
                var raw = File.ReadAllBytes(Path.Combine(dir, t.height));
                if (raw.Length != S * S * 4) throw new Exception(t.id + ": " + t.height + " has " + raw.Length + " bytes, expected " + S * S * 4 + " (truncated or edited)");
                if (t.sha256 != null && !string.IsNullOrEmpty(t.sha256.height))
                {
                    var hex = BitConverter.ToString(sha.ComputeHash(raw)).Replace("-", "").ToLowerInvariant();
                    if (hex != t.sha256.height) throw new Exception(t.id + ": " + t.height + " does not match the manifest's sha256 (corrupt or edited)");
                }
            }
    }

    public static void Import(string dir)
    {
        int maxLevel = MaxLevel;
        var m = JsonUtility.FromJson<Manifest>(File.ReadAllText(Path.Combine(dir, "world.manifest.json")));
        if (m.height_encoding.type != "float32") throw new Exception("unsupported height encoding " + m.height_encoding.type + " (expected float32, schema 2)");
        if (m.height_encoding.row_order != "north_to_south") throw new Exception("unsupported row_order " + m.height_encoding.row_order);
        int S = m.grid.samples_per_tile, res = Math.Max(16, Math.Min(2048, m.grid.tile_cells));
        float span = m.grid.tile_span_m, range = Math.Max(1f, m.height_encoding.max_m - m.height_encoding.min_m); // terrain stores heights as 0..1 of this range (Unity quantises to 16 bit itself)
        VerifyFiles(dir, m, maxLevel, S);
        int waterClasses = 0;
        foreach (var d in m.depth_classes) if (d.id > 0 && d.id != 255) waterClasses++;

        // Assets are updated in place, never deleted and recreated: their GUIDs (and so Addressables
        // AssetReferences, scene links and VCS history) stay stable across re-imports of the same tile ids.
        string gen = dir + "/Generated";
        if (!AssetDatabase.IsValidFolder(gen)) AssetDatabase.CreateFolder(dir, "Generated");
        foreach (var f in new[] { "Terrains", "Prefabs", "Layers", "Skirts" }) if (!AssetDatabase.IsValidFolder(gen + "/" + f)) AssetDatabase.CreateFolder(gen, f);

        var layers = MakeLayers(gen);
        Material skirtMat = maxLevel > 0 ? SkirtMaterial(gen) : null;
        // No terrain material template: the active render pipeline's default terrain material is used, so the
        // same TerrainData renders with whatever URP terrain shader the project uses. (A hand-made URP Terrain/Lit template rendered magenta beyond basemapDistance.)
        var index = ScriptableObject.CreateInstance<OceanWorldIndex>();
        index.worldName = m.name; index.buildId = m.build_id; index.epsg = m.crs.epsg;
        index.originEasting = m.grid.origin_easting;
        index.originNorthingSouth = m.grid.origin_northing_top - m.grid.tiles_y * (double)span;
        index.cellM = m.grid.cell_m; index.tileSpanM = span; index.tilesX = m.grid.tiles_x; index.tilesY = m.grid.tiles_y;
        index.minM = m.height_encoding.min_m; index.maxM = m.height_encoding.max_m; index.attribution = m.attribution;
        var byCell = new Dictionary<(int, int), TileInfo>();
        foreach (var t in m.tiles) if (t.level == 0) byCell[(t.col, t.row)] = t; // coarser levels are keyed separately (byLevelCell)
        string Id(int c, int r) => byCell.TryGetValue((c, r), out var t) ? t.id : "";
        var entries = new List<OceanTileEntry>();
        var lodEntries = new List<OceanTileEntry>();
        var byLevelCell = new Dictionary<(int, int, int), TileInfo>();
        foreach (var t in m.tiles) if (t.level > 0 && t.level <= maxLevel) byLevelCell[(t.level, t.col, t.row)] = t;
        string LodId(int l, int c, int r) => byLevelCell.TryGetValue((l, c, r), out var t) ? t.id : "";
        var prefabPaths = new Dictionary<string, string>();

        foreach (var t in m.tiles)
        {
            if (t.level > maxLevel) continue;
            float tspan = span * (1 << t.level); // coarser levels double the tile span (same sample count)
            byte[] raw = File.ReadAllBytes(Path.Combine(dir, t.height));
            if (raw.Length != S * S * 4) throw new Exception(t.id + ": expected " + S * S * 4 + " bytes, got " + raw.Length);
            var h = new float[S, S]; // h[z, x], z grows north; file rows run north to south
            float lo = m.height_encoding.min_m;
            for (int i = 0; i < S; i++)
                for (int j = 0; j < S; j++)
                {
                    float v = BitConverter.ToSingle(raw, (i * S + j) * 4); // little-endian host assumed (all Unity platforms)
                    if (v <= m.height_encoding.nodata + 1f) v = lo; // nodata cells are flat at the minimum; the depth class layer marks them
                    h[S - 1 - i, j] = Mathf.Clamp01((v - lo) / range);
                }

            string tdPath = gen + "/Terrains/" + t.id + ".asset";
            var td = AssetDatabase.LoadAssetAtPath<TerrainData>(tdPath);
            bool newTd = td == null;
            if (newTd) td = new TerrainData();
            td.heightmapResolution = S; td.size = new Vector3(tspan, range, tspan);
            td.SetHeights(0, 0, h);
            td.terrainLayers = layers;
            td.alphamapResolution = res;
            td.SetAlphamaps(0, 0, Paint(ReadMask(Path.Combine(dir, t.depth_class), S), ReadMask(Path.Combine(dir, t.shore_mask), S), S, res, waterClasses));
            if (newTd) AssetDatabase.CreateAsset(td, tdPath); else EditorUtility.SetDirty(td);

            // Tile row 0 is the northernmost; world +Z is north, y = 0 is sea level.
            var pos = t.level == 0 ? new Vector3(t.col * span, m.height_encoding.min_m, (m.grid.tiles_y - 1 - t.row) * span)
                : new Vector3((float)(t.bounds_utm[0] - index.originEasting), m.height_encoding.min_m, (float)(t.bounds_utm[1] - index.originNorthingSouth));
            var go = Terrain.CreateTerrainGameObject(td);
            go.name = t.id;
            go.transform.position = pos;
            if (skirtMat != null) AddSkirts(go, h, S, tspan / (S - 1), range, SkirtUpM, skirtMat, gen + "/Skirts/" + t.id + ".asset");
            string prefab = gen + "/Prefabs/" + t.id + ".prefab";
            PrefabUtility.SaveAsPrefabAsset(go, prefab);
            UnityEngine.Object.DestroyImmediate(go);
            prefabPaths[t.id] = prefab;

            (t.level == 0 ? entries : lodEntries).Add(new OceanTileEntry
            {
                id = t.id, level = t.level, col = t.col, row = t.row, position = pos, size = new Vector3(tspan, range, tspan),
                minM = t.min_m, maxM = t.max_m, minEasting = t.bounds_utm[0], minNorthing = t.bounds_utm[1], maxEasting = t.bounds_utm[2], maxNorthing = t.bounds_utm[3],
                waterFraction = t.water_fraction, measuredWaterFraction = t.measured_water_fraction, nodataFraction = t.nodata_fraction,
                address = "ocean/" + m.name + "/" + t.id,
                west = t.level == 0 ? Id(t.col - 1, t.row) : LodId(t.level, t.col - 1, t.row), east = t.level == 0 ? Id(t.col + 1, t.row) : LodId(t.level, t.col + 1, t.row),
                north = t.level == 0 ? Id(t.col, t.row - 1) : LodId(t.level, t.col, t.row - 1), south = t.level == 0 ? Id(t.col, t.row + 1) : LodId(t.level, t.col, t.row + 1),
            });
        }
        index.tiles = entries.ToArray();
        index.lodTiles = lodEntries.ToArray();
        index.maxLevelImported = maxLevel;
        index.lodMethod = m.lod != null ? m.lod.method : null;
        var cps = new List<OceanCheckpoint>();
        foreach (var c in m.checkpoints) cps.Add(new OceanCheckpoint { name = c.name, easting = c.easting, northing = c.northing, elevationM = (float)c.elevation_m });
        index.checkpoints = cps.ToArray();
        index.seamarks = ReadSeamarks(dir, index);
        string indexPath = gen + "/OceanWorldIndex.asset";
        var oldIndex = AssetDatabase.LoadAssetAtPath<OceanWorldIndex>(indexPath);
        if (oldIndex == null) AssetDatabase.CreateAsset(index, indexPath);
        else { EditorUtility.CopySerialized(index, oldIndex); UnityEngine.Object.DestroyImmediate(index); EditorUtility.SetDirty(oldIndex); }
        // Tiles that left the manifest (bbox shrank) are removed; the rest keep their assets.
        var keep = new HashSet<string>(prefabPaths.Keys);
        foreach (var sub in new[] { "Terrains", "Prefabs", "Skirts" })
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

        // One empty marker per seamark so a game can attach prefabs by type; positions come from the index.
        if (index.seamarks.Length > 0)
        {
            var smRoot = new GameObject("Seamarks");
            smRoot.transform.SetParent(root.transform);
            foreach (var s in index.seamarks)
            {
                var go = new GameObject(s.type + (s.name != "" ? ": " + s.name : " " + s.id));
                go.transform.SetParent(smRoot.transform);
                go.transform.position = s.position;
            }
        }

        var sea = GameObject.CreatePrimitive(PrimitiveType.Plane);
        sea.name = "SeaLevel";
        sea.transform.SetParent(root.transform);
        float width = m.grid.tiles_x * span, depth = m.grid.tiles_y * span;
        sea.transform.position = new Vector3(width / 2, 0, depth / 2);
        sea.transform.localScale = new Vector3(width / 10, 1, depth / 10);
        var mat = AssetDatabase.LoadAssetAtPath<Material>(gen + "/Sea.mat");
        // Rebuilt every import and copied into the existing asset (GUID stays): a material made by an older
        // importer or another pipeline would otherwise keep a shader URP cannot draw (pink sea).
        var fresh = TransparentMaterial(new Color(0.05f, 0.3f, 0.5f, 0.45f));
        if (mat == null) { mat = fresh; AssetDatabase.CreateAsset(mat, gen + "/Sea.mat"); }
        else { EditorUtility.CopySerialized(fresh, mat); EditorUtility.SetDirty(mat); }
        sea.GetComponent<Renderer>().sharedMaterial = mat;

        double maxErr = Verify(m, loaded, index, span);
        var scenePath = dir + "/" + m.name + ".unity";
        UnityEditor.SceneManagement.EditorSceneManager.SaveScene(scene, scenePath);
        EditorUtility.SetDirty(index);
        AssetDatabase.SaveAssets();
        TilesImported?.Invoke(index, prefabPaths);
        AssetDatabase.SaveAssets();
        Debug.Log("OCEAN_VERIFY checkpoints=" + m.checkpoints.Length + " max_err=" + maxErr.ToString("F4") + " m");
        Debug.Log("OCEAN_IMPORT_OK " + m.name + " tiles=" + index.tiles.Length + " (level 0; " + lodEntries.Count + " coarser tiles of " + m.tiles.Length + " in the manifest, max level " + maxLevel + ") scene=" + scenePath + " index=" + gen + "/OceanWorldIndex.asset");
    }

    // Convert each manifest checkpoint (UTM) to Unity space independently of tile placement
    // and compare the terrain height there with the expected elevation.
    static OceanSeamark[] ReadSeamarks(string dir, OceanWorldIndex index)
    {
        string path = dir + "/seamarks.geojson";
        if (!File.Exists(path)) return new OceanSeamark[0];
        var list = new List<OceanSeamark>();
        foreach (var f in JsonUtility.FromJson<SmFile>(File.ReadAllText(path)).features)
        {
            var p = f.properties;
            bool floating = p.seamark_type.StartsWith("buoy_") || p.seamark_type == "light_float" || p.seamark_type == "light_vessel";
            var pos = index.UtmToUnity(p.easting, p.northing, floating ? 0 : p.dem_elevation_m);
            var tile = index.TileAt(pos);
            if (tile == null) continue; // the Go side already drops marks outside the grid; this guards edge rounding
            list.Add(new OceanSeamark { id = p.osm_type + "/" + p.osm_id, type = p.seamark_type, name = p.name ?? "", position = pos, easting = p.easting, northing = p.northing, tileId = tile.id });
        }
        return list.ToArray();
    }

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
            if (err > Math.Max(Tolerance, (m.height_encoding.max_m - m.height_encoding.min_m) / 65535.0 * 1.01)) throw new Exception("checkpoint " + c.name + " expected " + c.elevation_m + " m, terrain gives " + y + " m: world is misplaced or flipped");
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
        if (Array.IndexOf(Environment.GetCommandLineArgs(), "-oceanHideSea") >= 0)
        {
            var sea = GameObject.Find("SeaLevel");
            if (sea != null) sea.SetActive(false);
        }
        var rt = new RenderTexture(1920, 1080, 24);
        cam.targetTexture = rt;
        var pipe = UnityEngine.Rendering.GraphicsSettings.currentRenderPipeline;
        Debug.Log("OCEAN_PIPELINE " + (pipe != null ? pipe.GetType().Name : "none"));
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
