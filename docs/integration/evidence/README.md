# Evidence scripts (2026-11-01)

Reproduce the measured (M) numbers in `../sailing-game-audit.md`. They read the game repo at `~/Documents/projects/Unity/Alone` (read-only) and need `python3` with `pyproj`, `numpy`, `scipy`, `Pillow`. The `.out` files are the outputs from the audit run; the game's pack data is local-only, so results depend on it.

| Script | Run from | Shows |
|---|---|---|
| `coast_alignment_and_projection.py` | OceanBuilder root (needs `output/falmouth`) | GSHHG (game) vs OSM (OB) coast distance at Falmouth; AEQD vs UTM scale and rotation per site |
| `leg_sizes_and_placement_error.py` | the game repo root | Leg lengths, UTM zones, cell counts, game-tile placement error vs true geo at Mindelo |
| `pack_relief_and_clusters.py` | the game repo root | Terrain y range per pack (flat), land clusters, extent and zones per cluster |
