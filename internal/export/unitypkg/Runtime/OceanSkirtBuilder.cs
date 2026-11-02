// Builds the crack-hiding skirts of a terrain tile (see OceanTileSkirts). Shared by the importer (which saves the meshes as
// assets) and the tests (which build skirts for synthetic terrains). Pure geometry from a heightmap; no asset access.
using UnityEngine;

namespace OceanBuilder.World
{
    public static class OceanSkirtBuilder
    {
        public static readonly string[] Names = { "Skirt_N", "Skirt_E", "Skirt_S", "Skirt_W" };

        /// <summary>A skirt hangs below its edge by the edge's own relief plus this many cells of the tile's level (at least 1 m).</summary>
        public const float DepthCells = 2f;

        /// <summary>
        /// One vertical strip per tile edge in the terrain's local space (x east, z north, origin at the tile's south-west corner).
        /// h is the heightmap as stored in TerrainData (h[z, x], 0..1 of range, S x S samples); cell is the sample spacing in metres.
        /// The strip's top is at the edge sample plus <paramref name="up"/>, its bottom <c>relief + DepthCells * cell</c> below the sample.
        /// Order: north (z = S-1), east (x = S-1), south (z = 0), west (x = 0).
        /// </summary>
        public static Mesh[] BuildMeshes(float[,] h, int S, float cell, float range, float up)
        {
            var meshes = new Mesh[4];
            for (int side = 0; side < 4; side++)
            {
                float lo = float.MaxValue, hi = float.MinValue;
                for (int i = 0; i < S; i++)
                {
                    float v = Sample(h, S, side, i) * range;
                    lo = Mathf.Min(lo, v); hi = Mathf.Max(hi, v);
                }
                float depth = Mathf.Max(1f, (hi - lo) + DepthCells * cell);
                var verts = new Vector3[S * 2]; var tris = new int[(S - 1) * 6];
                for (int i = 0; i < S; i++)
                {
                    int x = side == 1 ? S - 1 : side == 3 ? 0 : i, z = side == 0 ? S - 1 : side == 2 ? 0 : i;
                    float y = h[z, x] * range;
                    verts[i * 2] = new Vector3(x * cell, y + up, z * cell);
                    verts[i * 2 + 1] = new Vector3(x * cell, y - depth, z * cell);
                }
                for (int i = 0; i < S - 1; i++)
                {
                    int a = i * 2, b = a + 1, c = a + 2, d = a + 3, k = i * 6;
                    tris[k] = a; tris[k + 1] = c; tris[k + 2] = b; tris[k + 3] = c; tris[k + 4] = d; tris[k + 5] = b;
                }
                var m = new Mesh { name = Names[side], vertices = verts, triangles = tris };
                m.RecalculateBounds(); m.RecalculateNormals();
                meshes[side] = m;
            }
            return meshes;
        }

        static float Sample(float[,] h, int S, int side, int i) => h[side == 0 ? S - 1 : side == 2 ? 0 : i, side == 1 ? S - 1 : side == 3 ? 0 : i];

        /// <summary>Adds the four strips as disabled children of the terrain object and an OceanTileSkirts component that switches them.</summary>
        public static OceanTileSkirts Attach(GameObject terrainGo, Mesh[] meshes, Material mat)
        {
            var renderers = new Renderer[4];
            for (int side = 0; side < 4; side++)
            {
                var go = new GameObject(Names[side]);
                go.transform.SetParent(terrainGo.transform, false);
                go.AddComponent<MeshFilter>().sharedMesh = meshes[side];
                var r = go.AddComponent<MeshRenderer>();
                r.sharedMaterial = mat; r.shadowCastingMode = UnityEngine.Rendering.ShadowCastingMode.Off; r.receiveShadows = false; r.enabled = false;
                renderers[side] = r;
            }
            var sk = terrainGo.AddComponent<OceanTileSkirts>();
            sk.north = renderers[0]; sk.east = renderers[1]; sk.south = renderers[2]; sk.west = renderers[3];
            return sk;
        }
    }
}
