# Contributing

1. Install Go and the GDAL command-line tools.
2. `go vet ./... && go test ./...` must pass offline before and after your change.
3. Read [AGENTS.md](AGENTS.md) for the invariants (shared tile edges, no silent datum mixing, provenance on every source).
4. New data source: add provenance and licence handling, a small real-data fixture with a README naming its licence, and validator checks for what can go wrong with it.
5. Significant design changes get a short ADR in `docs/decisions/`.
6. Do not commit datasets beyond small test crops; check licences first.

Bug reports are most useful with the `world.json`, `ocean validate -json` output and the manifest `build_id`.
