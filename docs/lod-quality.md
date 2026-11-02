# LOD quality: what is measured, what the methods cost

Status: implemented and tested (`internal/validate/lod.go`, `TestLODMethodsOnHardFeatures`). Measured on one real
fixture (Falmouth, 3.6 % water) and one synthetic scene; other terrain will differ.

## What `ocean lod-report` measures

Each LOD level k is compared with level 0 **at every level-0 vertex**, sampling level k bilinearly as a consumer would.
All figures come from the tile files.

| Column | Meaning |
|---|---|
| rms / p95 / max | `\|level k - level 0\|` in metres |
| too deep | largest amount level k is deeper than level 0 in water (the unsafe direction for a shoal) |
| land lost / water lost | vertices that change side of the waterline: island or rock shrinking / channel or inlet closing |
| shallow | rms error where level-0 water is shallower than 20 m (a reporting convention, not a navigation limit) |
| class | fraction of vertices whose depth class (manifest bands) differs from level 0 |
| slope | rms east-west gradient error (rise over run) |
| uncovered | level-0 vertices with a value where level k has none |

Structural properties are checked by `ocean validate`, not by the report: neighbouring tiles of one level share edge
samples exactly (`edge-mismatch`), and every parent sample equals the child sample at the same position (`lod-mismatch`,
"nesting"). Both are errors, never thresholds.

Not measured: connectivity (the report counts vertices that switch side of the waterline; it does not count islands or
channels, so a 1-cell channel and a 100-cell inlet look alike), 2-D slope direction, and anything a renderer does with
the levels (morphing, skirts).

## Thresholds

`ocean lod-report <dir> -max-rms m -max-too-deep m -max-shallow-rms m -max-flip-fraction f -max-class-disagree f
[-check-up-to level]` exits non-zero and prints `EXCEEDED` lines when a limit is broken (`-json` adds `thresholds` and
`exceeded`). Zero or an absent flag means unchecked. There are no default limits: what is acceptable depends on the
game, and a default would be a claim about navigation safety this tool does not make.

## Falmouth fixture (real GMRT, 20 m cells, 4 levels, bilinear; 82 000 vertices compared)

Vertex decimation (default):

| level | cell m | rms m | p95 m | max m | too deep m | land lost | water lost | shallow rms m | class | slope |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | 40 | 0.319 | 0.699 | 4.12 | 1.90 | 9 | 25 | 0.250 | 0.0010 | 0.0172 |
| 2 | 80 | 0.964 | 2.016 | 10.97 | 7.07 | 37 | 115 | 0.735 | 0.0035 | 0.0315 |
| 3 | 160 | 2.405 | 5.028 | 15.20 | 6.35 | 70 | 339 | 1.573 | 0.0097 | 0.0457 |
| 4 | 320 | 5.069 | 10.434 | 21.38 | 1.14 | 32 | 841 | 3.542 | 0.0215 | 0.0574 |

`shoal`:

| level | cell m | rms m | p95 m | max m | too deep m | land lost | water lost | shallow rms m | class | slope |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | 40 | 0.382 | 0.797 | 6.99 | 0.000 | 3 | 127 | 1.095 | 0.0052 | 0.0180 |
| 2 | 80 | 1.085 | 2.252 | 10.97 | 0.000 | 4 | 563 | 2.646 | 0.0181 | 0.0318 |
| 3 | 160 | 2.537 | 5.409 | 15.20 | 0.000 | 7 | 1222 | 4.391 | 0.0315 | 0.0459 |
| 4 | 320 | 5.164 | 10.460 | 21.38 | 0.000 | 6 | 1672 | 5.978 | 0.0385 | 0.0574 |

(Output of `ocean lod-report` on builds of `testdata/falmouth`, 2026-11-02. Config: bbox -5.135..-5.06, 50.15..50.19.)

## Trade-offs, as measured

- **Decimation** keeps real level-0 samples and the waterline where a coarse vertex lands on it, but it is blind to
  features narrower than 2^k cells and can be deeper than level 0 in water by metres (7 m at level 2 here).
- **Shoal** removes the too-deep direction completely (0.000 at every level, by construction and measured) and costs
  accuracy where it matters for shallow water: the shallow-water rms is 1.7-4.4 times larger (1.1 vs 0.25 m at level 1), the
  depth-class disagreement 1.8-5.2 times larger, and **more channel vertices read as land** (water lost 127 vs 25 at level 1):
  the shallowed samples next to the shore interpolate to land sooner. It is a bias, not an accuracy method.
- Neither method is safe for navigation. "Never deeper than level 0 at level-0 vertices" is a property of the
  sampled surface at those vertices, not a clearance guarantee between them, and level 0 is itself interpolated
  from a source whose resolution the manifest records.
- On the synthetic hard-feature scene (30 m channel through land, 25 x 35 m islet, 30 x 45 m shoal, cliff coast) the test
  asserts that decimation loses the islet and closes the channel and that shoal is never deeper.

Reproduce: `ocean build <config> -force && ocean lod-report <out>`; the configs are plain `local` sources over the fixture
files with `"lod_levels": 4` and, for shoal, `"lod_method": "shoal"`.
