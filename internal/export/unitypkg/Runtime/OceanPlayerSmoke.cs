// Runs inside a built player: samples every imported terrain at its centre, compares the height with the tile index,
// prints one OCEAN_PLAYER_SMOKE line and quits. Added to a scene copy by OceanStandaloneBuild; it is also a small
// example of querying an imported world at runtime.
using System.Text;
using UnityEngine;

namespace OceanCompiler.World
{
    public class OceanPlayerSmoke : MonoBehaviour
    {
        public OceanWorldIndex index;
        public float toleranceM = 0.5f;

        void Start()
        {
            var sb = new StringBuilder();
            int ok = 0, bad = 0, noTerrain = 0;
            foreach (var t in index.tiles)
            {
                if (t.level != 0) continue;
                var go = GameObject.Find(t.id);
                var terrain = go ? go.GetComponentInChildren<Terrain>() : null;
                if (terrain == null) { noTerrain++; continue; }
                var centre = terrain.transform.position + new Vector3(t.size.x / 2, 0, t.size.z / 2);
                float h = terrain.SampleHeight(centre) + terrain.transform.position.y;
                // the tile's elevation range is known from the manifest: the sample must lie inside it
                if (h >= t.minM - toleranceM && h <= t.maxM + toleranceM) ok++;
                else { bad++; sb.Append(' ').Append(t.id).Append('=').Append(h.ToString("F1")); }
            }
            Debug.Log("OCEAN_PLAYER_SMOKE world=" + index.worldName + " build=" + index.buildId.Substring(0, 12) +
                      " tiles=" + index.tiles.Length + " ok=" + ok + " outside_range=" + bad + " missing_terrain=" + noTerrain + sb);
            Application.Quit(bad == 0 && noTerrain == 0 && ok > 0 ? 0 : 3);
        }
    }
}
