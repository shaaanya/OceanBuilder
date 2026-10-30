# 0002: UTM grid, shared-edge tiles, 16-bit RAW heights

Status: accepted, 2026-10-30

## Decision

- Work in the UTM zone of the bbox centre (metres). One zone per world; worlds spanning zones are not supported yet.
- Vertex-registered grid; tiles hold `N+1` samples with `N` a power of two, adjacent tiles share the edge row/column.
- Tile ids are absolute (`floor(E/span)`, `floor(N/span)`), so adding area to a world does not rename existing tiles.
- Heights are uint16 little-endian normalised over the world's min/max, rows north to south, plus manifest min/max. Unity terrains import 2ⁿ+1 heightmaps and map 0..1 over terrain height; importer sets terrain y = min_m so y = 0 is sea level.
- JSON config (`world.json`) rather than YAML: standard library only, no dependency.

## Consequences

- Crack-free by construction and checked by `validate`.
- 16-bit quantisation over a 330 m range is ≈5 mm; worlds with huge relief (deep trenches plus mountains) lose precision. Per-tile ranges or 32-bit float export can be added when needed.
- Unreal Landscape needs different sizes (2ⁿ−1 per component); a separate exporter will resample from the manifest grid, not change the core format.
