using System.Collections.Generic;
using UnityEngine;

namespace OceanCompiler.World
{
    /// <summary>Which tiles to load and unload around a position. Pure function of the index: no Addressables, no scene state.</summary>
    public static class OceanStreamingPlan
    {
        /// <summary>Horizontal distance in metres from a Unity-space point to a tile's rectangle (0 inside it).</summary>
        public static float DistanceXZ(OceanTileEntry t, Vector3 p)
        {
            float dx = Mathf.Max(t.position.x - p.x, 0f, p.x - (t.position.x + t.size.x));
            float dz = Mathf.Max(t.position.z - p.z, 0f, p.z - (t.position.z + t.size.z));
            return Mathf.Sqrt(dx * dx + dz * dz);
        }

        /// <summary>
        /// Level-0 tiles closer than loadRadiusM that are not in <paramref name="have"/> go to toLoad (nearest first);
        /// tiles in have that are farther than unloadRadiusM go to toUnload. unloadRadiusM is raised to loadRadiusM
        /// so a tile on the boundary cannot flip every frame.
        /// </summary>
        public static void Compute(OceanWorldIndex index, Vector3 p, float loadRadiusM, float unloadRadiusM,
            ICollection<string> have, List<OceanTileEntry> toLoad, List<string> toUnload)
        {
            toLoad.Clear(); toUnload.Clear();
            unloadRadiusM = Mathf.Max(unloadRadiusM, loadRadiusM);
            var dist = new Dictionary<string, float>();
            foreach (var t in index.tiles)
            {
                if (t.level != 0) continue;
                float d = DistanceXZ(t, p);
                dist[t.id] = d;
                if (!have.Contains(t.id) && d <= loadRadiusM) toLoad.Add(t);
            }
            foreach (var id in have)
                if (!dist.TryGetValue(id, out var d) || d > unloadRadiusM) toUnload.Add(id);
            toLoad.Sort((a, b) => dist[a.id].CompareTo(dist[b.id]));
        }
    }
}
