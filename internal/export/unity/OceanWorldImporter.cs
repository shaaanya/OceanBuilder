// Ocean Compiler Unity importer. Thin adapter: all processing happens in the
// Go core; this reads world.manifest.json and the 16-bit tile heightmaps and
// builds one Terrain per tile (neighbours linked, so no seams) plus a sea-level plane.
// Batchmode: Unity -batchmode -projectPath <p> -executeMethod OceanWorldImporter.ImportFromCommandLine -quit
using System;
using System.IO;
using UnityEditor;
using UnityEngine;

public static class OceanWorldImporter
{
    [Serializable] class Grid { public int tile_cells; public int samples_per_tile; public float tile_span_m; public int tiles_x; public int tiles_y; public float cell_m; }
    [Serializable] class Height { public float min_m; public float max_m; public string row_order; }
    [Serializable] class TileInfo { public string id; public int col; public int row; public string height; }
    [Serializable] class Manifest { public string name; public Grid grid; public Height height_encoding; public TileInfo[] tiles; }

    [MenuItem("Ocean/Import World")]
    public static void ImportMenu()
    {
        foreach (var dir in Directory.GetDirectories("Assets/OceanWorld")) Import(dir);
    }

    // Imports every world under Assets/OceanWorld; exits non-zero on failure in batchmode.
    public static void ImportFromCommandLine()
    {
        try { ImportMenu(); }
        catch (Exception e) { Debug.LogError("Ocean import failed: " + e); EditorApplication.Exit(1); }
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
            AssetDatabase.CreateAsset(td, terrainDir + "/" + t.id + ".asset");
            var go = Terrain.CreateTerrainGameObject(td);
            go.name = t.id;
            go.transform.SetParent(root.transform);
            // Tile row 0 is the northernmost; world +Z is north, y=0 is sea level.
            go.transform.position = new Vector3(t.col * span, m.height_encoding.min_m, (m.grid.tiles_y - 1 - t.row) * span);
            grid[t.col, t.row] = go.GetComponent<Terrain>();
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
        var mat = new Material(Shader.Find("Standard")) { color = new Color(0.1f, 0.35f, 0.55f, 0.6f) };
        AssetDatabase.CreateAsset(mat, dir + "/Sea.mat");
        sea.GetComponent<Renderer>().sharedMaterial = mat;

        var scenePath = dir + "/" + m.name + ".unity";
        UnityEditor.SceneManagement.EditorSceneManager.SaveScene(scene, scenePath);
        AssetDatabase.SaveAssets();
        Debug.Log("OCEAN_IMPORT_OK " + m.name + " tiles=" + m.tiles.Length + " scene=" + scenePath);
    }
}
