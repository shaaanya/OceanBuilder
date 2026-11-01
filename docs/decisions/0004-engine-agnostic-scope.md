# 0004: OceanBuilder is a standalone tool; no game-specific work

Date: 2026-11-01. Status: accepted.

## Context

An earlier session audited one particular game and drew up an integration plan (game-frame grid export, conforming to the game's coastline, a runtime depth sampler for its code). The plan shaped the roadmap. The product goal is a tool any game developer can adopt; designing the core around one game's coordinate frame, terrain system or coastline would make it worse for everyone else and would couple the tool to a project it cannot test against.

## Decision

- Remove `docs/integration/` and the roadmap section built on it. The audit stays in git history only.
- The core owns geography: sources, projection, merging, tiling, LOD, validation, provenance, caches, manifest. It writes one documented format (`docs/formats/world-v2.md`).
- Anything that depends on an engine or a project is an adapter that reads that format: `internal/export` (Unity, URP only) and the GeoTIFF exporter. A game with its own world frame converts from the UTM grid using the format's documented CRS; OceanBuilder does not learn about it.
- A public Go package, `pkg/oceanworld`, reads the format (open, sample, profile, UTM conversion) without GDAL, so a consumer, a test or another language port has a reference.

## Consequences

Per-game needs (own coastline source, own frame) are solved by the consumer using the format and `pkg/oceanworld`, or by a generic feature (local sources, `utm_epsg`), never by a special case in the core. Unreal or other engine adapters wait until the format has been used by a second consumer.
