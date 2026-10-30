// Ocean Compiler Unity importer. Thin adapter: all processing happens in the
// Go core; this reads world.manifest.json and the 16-bit tile heightmaps and
// builds one Terrain per tile (neighbours linked, so no seams) plus a sea-level plane.
//
// Batchmode:
//   Unity -batchmode -projectPath <p> -executeMethod OceanWorldImporter.ImportFromCommandLine -quit
// Optional in-engine screenshot (needs a GPU, so do not pass -nographics):
//   ... -oceanShot <path.png>
// Import verifies orientation against manifest checkpoints and exits non-zero on mismatch.
using System;
using System.IO;
using UnityEditor;
using UnityEngine;

public static class OceanWorldImporter
{
    [Serializable] class Grid { public int tile_cells; public int samples_per_tile; public float tile_span_m; public int tiles_x; public int tiles_y; public float cell_m; public double origin_easting; public double origin_northing_top; }
    [Serializable] class Height { public float min_m; public float max_m; public string row_order; }
    [Serializable] class TileInfo { public string id; public int col; public int row; public string height; }
    [Serializable] class Checkpoint { public string name; public double easting; public double northing; public double elevation_m; }
    [Serializable] class Manifest { public string name; public Grid grid; public Height height_encoding; public TileInfo[] tiles; public Checkpoint[] checkpoints; }

    const double Tolerance = 0.05; // metres; 16-bit quantisation over typical ranges is ~0.005 m

    [MenuItem("Ocean/Import World")]
    public static void ImportMenu()
    {
        foreach (var dir in Directory.GetDirectories("Assets/OceanWorld")) Import(dir);
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

    public static void Import(string dir)
    {
        var m = JsonUtility.FromJson<Manifest>(File.ReadAllText(Path.Combine(dir, "world.manifest.json")));
        if (m.height_encoding.row_order != "north_to_south") throw new Exception("unsupported row_order " + m.height_encoding.row_order);
        int S = m.grid.samples_per_tile;
        float span = m.grid.tile_span_m, range = m.height_encoding.max_m - m.height_encoding.min_m;
        string terrainDir = dir + "/Terrains";
        Directory.CreateDirectory(terrainDir);

        var scene = UnityEditor.SceneManagement.EditorSceneManager.NewScene(UnityEditor.SceneManagement.NewSceneSetup.DefaultGameObjects, UnityEditor.SceneManagement.NewSceneMode.Single);
        var root = new GameObject("Ocean_" + m.name);

        // One flat-colour layer so terrain is not unlit/pink under any render pipeline.
        var tex = new Texture2D(4, 4); var px = new Color[16];
        for (int k = 0; k < 16; k++) px[k] = new Color(0.55f, 0.5f, 0.35f);
        tex.SetPixels(px); tex.Apply();
        AssetDatabase.CreateAsset(tex, dir + "/Seabed.asset");
        var layer = new TerrainLayer { diffuseTexture = tex, tileSize = new Vector2(100, 100) };
        AssetDatabase.CreateAsset(layer, dir + "/Seabed.terrainlayer");
        var tShader = Shader.Find("Universal Render Pipeline/Terrain/Lit");

        var grid = new Terrain[m.grid.tiles_x, m.grid.tiles_y];
        foreach (var t in m.tiles)
        {
            byte[] raw = File.ReadAllBytes(Path.Combine(dir, t.height));
            if (raw.Length != S * S * 2) throw new Exception(t.id + ": expected " + S * S * 2 + " bytes, got " + raw.Length);
            var h = new float[S, S]; // h[z, x], z grows north; file rows run north to south
            for (int i = 0; i < S; i++)
                for (int j = 0; j < S; j++)
                    h[S - 1 - i, j] = (raw[(i * S + j) * 2] | (raw[(i * S + j) * 2 + 1] << 8)) / 65535f;

            var td = new TerrainData { heightmapResolution = S, size = new Vector3(span, range, span) };
            td.SetHeights(0, 0, h);
            td.terrainLayers = new[] { layer };
            AssetDatabase.CreateAsset(td, terrainDir + "/" + t.id + ".asset");
            var go = Terrain.CreateTerrainGameObject(td);
            go.name = t.id;
            go.transform.SetParent(root.transform);
            // Tile row 0 is the northernmost; world +Z is north, y=0 is sea level.
            go.transform.position = new Vector3(t.col * span, m.height_encoding.min_m, (m.grid.tiles_y - 1 - t.row) * span);
            var terrain = go.GetComponent<Terrain>();
            if (tShader != null) terrain.materialTemplate = new Material(tShader);
            grid[t.col, t.row] = terrain;
        }
        for (int c = 0; c < m.grid.tiles_x; c++)
            for (int r = 0; r < m.grid.tiles_y; r++)
                grid[c, r].SetNeighbors(c > 0 ? grid[c - 1, r] : null, r > 0 ? grid[c, r - 1] : null,
                                        c < m.grid.tiles_x - 1 ? grid[c + 1, r] : null, r < m.grid.tiles_y - 1 ? grid[c, r + 1] : null);

        // Sea-level plane (Unity plane is 10x10 units at scale 1).
        var sea = GameObject.CreatePrimitive(PrimitiveType.Plane);
        sea.name = "SeaLevel";
        sea.transform.SetParent(root.transform);
        float w = m.grid.tiles_x * span, d = m.grid.tiles_y * span;
        sea.transform.position = new Vector3(w / 2, 0, d / 2);
        sea.transform.localScale = new Vector3(w / 10, 1, d / 10);
        var mat = TransparentMaterial(new Color(0.05f, 0.3f, 0.5f, 0.55f));
        AssetDatabase.CreateAsset(mat, dir + "/Sea.mat");
        sea.GetComponent<Renderer>().sharedMaterial = mat;

        double maxErr = Verify(m, grid, span);
        var scenePath = dir + "/" + m.name + ".unity";
        UnityEditor.SceneManagement.EditorSceneManager.SaveScene(scene, scenePath);
        AssetDatabase.SaveAssets();
        Debug.Log("OCEAN_VERIFY checkpoints=" + m.checkpoints.Length + " max_err=" + maxErr.ToString("F4") + " m");
        Debug.Log("OCEAN_IMPORT_OK " + m.name + " tiles=" + m.tiles.Length + " scene=" + scenePath);
    }

    // Convert each manifest checkpoint (UTM) to Unity space independently of tile placement
    // and compare the terrain height there with the expected elevation.
    static double Verify(Manifest m, Terrain[,] grid, float span)
    {
        if (m.checkpoints == null || m.checkpoints.Length == 0) throw new Exception("manifest has no checkpoints");
        double south = m.grid.origin_northing_top - m.grid.tiles_y * (double)span, maxErr = 0;
        foreach (var c in m.checkpoints)
        {
            double x = c.easting - m.grid.origin_easting, z = c.northing - south;
            int col = Math.Min((int)Math.Floor(x / span), m.grid.tiles_x - 1), rowFromSouth = Math.Min((int)Math.Floor(z / span), m.grid.tiles_y - 1);
            var terrain = grid[col, m.grid.tiles_y - 1 - rowFromSouth];
            var world = new Vector3((float)x, 0, (float)z);
            double y = terrain.SampleHeight(world) + terrain.transform.position.y;
            double err = Math.Abs(y - c.elevation_m);
            maxErr = Math.Max(maxErr, err);
            Debug.Log("OCEAN_CHECK " + c.name + " expected=" + c.elevation_m.ToString("F3") + " got=" + y.ToString("F3"));
            if (err > Tolerance) throw new Exception("checkpoint " + c.name + " expected " + c.elevation_m + " m, terrain gives " + y + " m: world is misplaced or flipped");
        }
        return maxErr;
    }

    static void Screenshot(string path)
    {
        var terrains = UnityEngine.Object.FindObjectsByType<Terrain>(FindObjectsSortMode.None);
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
        cam.targetTexture = rt; cam.Render();
        RenderTexture.active = rt;
        var img = new Texture2D(rt.width, rt.height, TextureFormat.RGB24, false);
        img.ReadPixels(new Rect(0, 0, rt.width, rt.height), 0, 0); img.Apply();
        Directory.CreateDirectory(Path.GetDirectoryName(Path.GetFullPath(path)));
        File.WriteAllBytes(path, img.EncodeToPNG());
        RenderTexture.active = null;
        Debug.Log("OCEAN_SHOT " + path);
    }
}
