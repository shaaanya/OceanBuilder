# EGM96 discrepancy: investigation (2026-11-02)

**Question.** 0.3.0 recorded that the installed EGM96 grid gives N = 36.021 m at 12.5E 55.5N while "an older PROJ regression value" is 36.394 m (and 20.757 m vs 21.153 m at 180E 0N). Which is right?

**Answer.** The installed grid is right; the PROJ gie values are not EGM96 undulations of those points. Remaining uncertainty is the interpolation error of a 15' grid (centimetres), not the grid.

## Evidence (all re-runnable)

Files: `/opt/homebrew/share/proj/us_nga_egm96_15.tif` (PROJ 9.x data, 1440 x 721, point registration, origin -180.125/90.125, "Converted from egm96_15.gtx (last modified 2018/10/05)", NGA public domain). GDAL 3.13.0.

| Point | PROJ grid (bilinear) | GeographicLib grid `egm96-15.pgm`, cubic | **Spherical-harmonic synthesis** (`Gravity -n egm96 -H`, EGM96 coefficients) | PROJ gie file |
|---|---|---|---|---|
| 12.5E 55.5N (grid node) | 36.0213 | 36.0247 | **36.0213** | 36.394 |
| 180E 0N (grid node) | 21.1533 | 21.1506 | **21.1533** | 20.757 |

- At grid nodes interpolation plays no role, and the installed grid equals the coefficient synthesis to 0.1 mm. The synthesis is the independent arbiter: it is computed from the EGM96 spherical-harmonic coefficients (GeographicLib `egm96.egm`; sha256 of the tarball `6fea4c6bd56ff8ac53dbdad8d5dd505c855471d0354c4abc5c5fe048bf8350c1`), not from any grid.
- NGA's own EGM96 test file (five points, as reproduced in Orekit's `GeoidTest`, "taken from the NGA's test file"; I did not reach NGA's site, which was unreachable from this machine): installed grid minus NGA = +0.019, +0.003, -0.042, +0.056, -0.030 m. Off-node points differ by up to 6 cm because PROJ interpolates a 15' grid bilinearly; the coefficient synthesis agrees with the NGA values to 4 mm or better at the same points.
- PROJ's own `gie` run on the unchanged upstream file `test/gie/more_builtins.gie` against this install FAILS (`deviation 372.8 mm` and `396.8 mm`). The same file's `multiplier=0.1` block expects 3.6021305 = 0.1 x 36.0213, i.e. the installed value, with a 15 cm tolerance that would also pass 3.639.
- Excluded causes: swapped coordinates (55.5E 12.5N gives 42.37, not 36.39); EGM2008 (36.475 at the same point); a half-cell registration shift (the nearest of the eight half-cell offsets gives 36.338, 0.056 m off, and none reproduces 20.757); sign or unit mistakes (the discrepancy is +0.37 m at one point and -0.40 m at the other).
- The gie expectation is not reproduced by any EGM96 computation tried. Its origin (an older conversion of the `egm96_15.gtx` test grid) was not established and I did not find a primary source for it; it is recorded here as unexplained, not as an error in PROJ's shipped grid.

## Consequence for OceanBuilder

- The builder uses the PROJ grid exactly as `gdaltransform` does. The value it applies is the one that agrees with the coefficient synthesis and with NGA's test file.
- `TestEGM96IndependentReferences` (`internal/world/compose_test.go`) asserts grid nodes to 2 mm against the synthesis and the NGA points to 0.10 m. It replaces the old 0.5 m test, which could not catch a 0.4 m error.
- Residual error of an EGM96-referenced conversion: up to about 6 cm from interpolation. The geoid itself is a model (EGM96 vs EGM2008 differ by 0.1-0.4 m in the examples above); neither is "mean sea level" at a coast within that range.

## Reproduce

```
brew install geographiclib proj
curl -L -o egm96.tar.bz2 https://sourceforge.net/projects/geographiclib/files/gravity-distrib/egm96.tar.bz2/download
tar xjf egm96.tar.bz2 && echo "55.5 12.5" | Gravity -d gravity -n egm96 -H        # 36.0213
echo "12.5 55.5 0" | gdaltransform -s_srs EPSG:4979 -t_srs EPSG:4326+5773          # -36.0213 (H = h - N)
go test ./internal/world -run EGM96 -v
```
