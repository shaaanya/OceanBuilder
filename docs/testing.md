# Testing tiers and the local verification procedure

Hosted CI (`.github/workflows/ci.yml`) runs the offline tier on ubuntu, windows and macos, the POSIX scripts on ubuntu and macos, the Unity LOD logic on ubuntu and a cross-compile check; the real-service tier runs weekly and on manual dispatch. First green run: 2026-11-02, commit `53d91f4`, after three fixes the first run exposed (Go 1.22.12 binaries abort on the macOS runner, `gdal_calc.py` could not be launched on Windows, no EXR driver in the conda-forge GDAL). The conda-forge GDAL used there has no EXR driver on Linux and Windows, so the LibreTerrain round-trip tests skip wherever the driver is missing (`requireEXR`) and are covered only on a machine whose GDAL has OpenEXR (the development Mac's does). The golden tile hashes run on amd64 (Linux, Windows) and arm64 (macOS), which is the floating-point check the matrix exists for. Not run anywhere: Unity Editor and player tests outside macOS, IL2CPP. `scripts/verify-local.sh` is the same procedure for a developer machine.

| Tier | What | Command | Needs | Time (macOS arm64) | In CI |
|---|---|---|---|---|---|
| fast | vet + unit and small integration tests; the ~10 slowest tests skip under `-short` | `scripts/verify-local.sh fast` | Go, GDAL | about 75 s | optional |
| full (integration) | everything offline, incl. resume at every stage, golden tile hashes, GeoTIFF agreement, LibreTerrain (runs the real app if `../LibreTerrain/libreterrain-mcp` exists) | `scripts/verify-local.sh full` | Go, GDAL | about 3.2 min | `offline` job, 3 OSes |
| cross | vet and compile every package and test binary for linux/windows/darwin x amd64/arm64 (does not run them) | `scripts/verify-local.sh cross` | Go | about 1 min | `cross-vet` |
| scripts | real processes: SIGINT/SIGKILL resume, 3 concurrent builds, Python reference reader against `ocean sample` | `scripts/verify-local.sh scripts` | Go, GDAL, Python 3 | about 2 min | `posix-scripts` |
| lod | 6635 property checks of the Unity LOD code, no Unity | `scripts/verify-local.sh lod` | .NET SDK 10 | 10 s | `unity-lod-logic` |
| smoke | real GMRT/ETOPO/GEBCO/CUDEM/Overpass | `scripts/verify-local.sh smoke` | network | minutes, may flake | schedule / dispatch |
| expensive | region ladder with real downloads (timings, memory, disk) | `python3 scripts/ladder.py bin/ocean <scratch> examples/regions/*.world.json` | network, GDAL, POSIX | 15-30 min | no |
| unity | PlayMode tests and a standalone player run | below | licensed Unity 6000.4 | 5-10 min | no |

Tests never touch the network unless `OCEAN_SMOKE=1`. `go test -short` is the only switch inside `go test`.

## Unity (manual; needs a licensed Editor)

```bash
ocean build lodw.world.json                       # a world with lod_levels >= 3
ocean export <world> -target unity -level 3 -dest examples/unity-demo
UNITY=/Applications/Unity/Hub/Editor/6000.4.0f1/Unity.app/Contents/MacOS/Unity
$UNITY -batchmode -projectPath examples/unity-demo -executeMethod OceanWorldImporter.ImportFromCommandLine -oceanMaxLevel 3 -quit
$UNITY -batchmode -projectPath examples/unity-demo -executeMethod OceanCompiler.World.Editor.OceanAddressables.BuildAndCheckFromCommandLine -quit   # or menu: Ocean > Addressables > Build And Check
$UNITY -batchmode -projectPath examples/unity-demo -runTests -testPlatform PlayMode -testResults results.xml
$UNITY -batchmode -projectPath examples/unity-demo -executeMethod OceanCompiler.World.Editor.OceanStandaloneBuild.BuildFromCommandLine -oceanScene Assets/OceanWorld/<name>/<name>.unity -oceanOut player/<name>.app -quit
player/<name>.app/Contents/MacOS/<exe> -batchmode      # with graphics: the seam probe renders; exit 0 and OCEAN_PLAYER_RESULT failures=0
```

The demo project embeds a **copy** of the package in `Packages/com.oceancompiler.world` (git-ignored, written by `ocean export -target unity`). After editing `internal/export/unitypkg` re-export, or the project keeps building the old code (this cost one wrong player run).

## Windows

No Windows test has been executed. What is covered: the code compiles and vets for windows/amd64 and windows/arm64 (`cross`), and it was read for Windows hazards:

- Only `filepath` is used for files; no `:` or other reserved characters in cache or output file names (ids are `L0_x_y`).
- `os.Rename` of a directory onto an existing one fails on Windows, as on POSIX; the code moves the old output aside first and rolls back on failure, and the stage cache falls back to verifying the other builder's stage.
- Windows refuses to rename or delete files that another process holds open (a viewer on `preview.png`, antivirus, an open Explorer window on the output): the build then fails at the final swap with the OS error and leaves the previous output untouched. Close the viewer and rebuild; the stages are resumed.
- Downloads use unique temp names (fixed 0.5.0-rc1; before, two builds sharing a cache and downloading the same file wrote the same `.part`).
- Signals: Ctrl-C is handled; SIGTERM does not exist on Windows. `scripts/interrupt_resume.py` and `concurrent_builds.py` are POSIX only. The interrupt behaviour on Windows is therefore **unverified**.
- GDAL: the CI installs it from conda-forge; `gdalwarp` etc. must resolve on `PATH` (`.exe` is found by `exec.LookPath`).
