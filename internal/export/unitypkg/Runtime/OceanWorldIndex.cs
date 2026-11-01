// Runtime tile index for a world built by the Ocean Compiler. It carries no
// geometry: tile terrain lives in separate prefabs (one per tile) that the game
// loads however it likes, typically through Addressables using Entry.address.
using System;
using UnityEngine;

namespace OceanCompiler.World
{
    /// <summary>One tile. Unity space: x = east, z = north, y = metres above sea level; the world's south-west corner is (0,0).</summary>
    [Serializable]
    public class OceanTileEntry
    {
        public string id;              // stable, absolute: L<level>_<floor(E/span)>_<floor(N/span)>
        public int level;              // LOD level; cell size and tile span double per level
        public int col, row;           // column from west, row from NORTH (manifest convention)
        public Vector3 position;       // south-west corner of the tile in Unity space (y = world min elevation)
        public Vector3 size;           // tile extents in metres (x, vertical range, z)
        public float minM, maxM;       // elevation range actually present in this tile
        public double minEasting, minNorthing, maxEasting, maxNorthing; // tile bounds in the world's UTM CRS
        public float waterFraction, measuredWaterFraction, nodataFraction;
        public string address;         // Addressables address of the tile prefab
        public string north, south, east, west; // neighbour ids at the same level; empty at the world edge
    }

    /// <summary>A manifest vertex with its expected elevation, for runtime sanity checks of a loaded world.</summary>
    [Serializable]
    public class OceanCheckpoint
    {
        public string name;
        public double easting, northing;
        public float elevationM;
    }

    /// <summary>An OpenSeaMap feature (buoy, light, harbour, wreck...) from seamarks.geojson. Volunteer-mapped; not for navigation.</summary>
    [Serializable]
    public class OceanSeamark
    {
        public string id;              // "<osm type>/<osm id>"
        public string type;            // seamark:type, e.g. buoy_lateral
        public string name;
        public Vector3 position;       // Unity space; y = 0 for floating marks, else DEM elevation
        public double easting, northing;
        public string tileId;          // tile containing the mark
    }

    public class OceanWorldIndex : ScriptableObject
    {
        public string worldName, buildId;
        public int epsg;               // CRS of the UTM coordinates below (metres)
        public double originEasting;   // UTM easting of Unity x = 0
        public double originNorthingSouth; // UTM northing of Unity z = 0
        public float cellM, tileSpanM, minM, maxM;
        public int tilesX, tilesY;
        public string[] attribution;
        public OceanTileEntry[] tiles;       // level 0
        public OceanTileEntry[] lodTiles = new OceanTileEntry[0]; // coarser levels, imported when the importer runs with a maximum level above 0
        public int maxLevelImported;
        public OceanCheckpoint[] checkpoints;
        public OceanSeamark[] seamarks = new OceanSeamark[0];

        public OceanTileEntry Get(string id)
        {
            foreach (var t in tiles) if (t.id == id) return t;
            return null;
        }

        /// <summary>Coarse tiles of one level (1 = twice the cell size); empty when that level was not imported.</summary>
        public System.Collections.Generic.IEnumerable<OceanTileEntry> LodTiles(int level)
        {
            foreach (var t in lodTiles) if (t.level == level) yield return t;
        }

        public Vector3 UtmToUnity(double easting, double northing, double elevationM = 0) =>
            new Vector3((float)(easting - originEasting), (float)elevationM, (float)(northing - originNorthingSouth));

        public void UnityToUtm(Vector3 p, out double easting, out double northing)
        {
            easting = originEasting + p.x;
            northing = originNorthingSouth + p.z;
        }

        /// <summary>Tile containing a Unity-space position, or null outside the world.</summary>
        public OceanTileEntry TileAt(Vector3 p)
        {
            // The east and north world edges belong to the last tile (tiles share edge samples).
            if (p.x < 0 || p.z < 0 || p.x > tilesX * tileSpanM || p.z > tilesY * tileSpanM) return null;
            int c = Mathf.Min(Mathf.FloorToInt(p.x / tileSpanM), tilesX - 1), r = Mathf.Min(Mathf.FloorToInt(p.z / tileSpanM), tilesY - 1);
            int row = tilesY - 1 - r;
            foreach (var t in tiles) if (t.col == c && t.row == row) return t;
            return null;
        }

        /// <summary>Link a freshly loaded terrain to whichever neighbours are loaded, and the neighbours back to it, so tile edges stay seamless.</summary>
        public void LinkNeighbours(OceanTileEntry e, Terrain terrain, Func<string, Terrain> loaded)
        {
            Terrain Find(string id) => string.IsNullOrEmpty(id) ? null : loaded(id);
            Terrain w = Find(e.west), n = Find(e.north), ea = Find(e.east), s = Find(e.south);
            terrain.SetNeighbors(w, n, ea, s);
            // Terrain neighbours are set per terrain; refresh the ones that already exist.
            if (w != null) w.SetNeighbors(Find(Get(e.west).west), Find(Get(e.west).north), terrain, Find(Get(e.west).south));
            if (ea != null) ea.SetNeighbors(terrain, Find(Get(e.east).north), Find(Get(e.east).east), Find(Get(e.east).south));
            if (n != null) n.SetNeighbors(Find(Get(e.north).west), Find(Get(e.north).north), Find(Get(e.north).east), terrain);
            if (s != null) s.SetNeighbors(Find(Get(e.south).west), terrain, Find(Get(e.south).east), Find(Get(e.south).south));
        }
    }
}
