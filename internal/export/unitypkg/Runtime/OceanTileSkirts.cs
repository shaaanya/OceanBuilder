// The crack-hiding skirts of one tile. The importer builds one thin vertical strip per tile edge (a child of the tile prefab,
// hanging below the edge samples and reaching a little above them) and adds this component to the prefab root; OceanLodStreamer
// switches an edge on only where the drawn neighbour is at another level. Unity's terrain neighbours stitch tiles of the same
// size, not a tile beside one twice its size; the skirt hides the gap that opens there (measured: docs/unity-lod.md).
using UnityEngine;

namespace OceanCompiler.World
{
    public class OceanTileSkirts : MonoBehaviour
    {
        public Renderer north, east, south, west;
        OceanSides current = (OceanSides)(-1);

        public OceanSides Current => current == (OceanSides)(-1) ? OceanSides.None : current;

        /// <summary>Shows exactly the skirts in <paramref name="sides"/>.</summary>
        public void Apply(OceanSides sides)
        {
            if (sides == current) return;
            current = sides;
            if (north) north.enabled = (sides & OceanSides.North) != 0;
            if (east) east.enabled = (sides & OceanSides.East) != 0;
            if (south) south.enabled = (sides & OceanSides.South) != 0;
            if (west) west.enabled = (sides & OceanSides.West) != 0;
        }
    }
}
