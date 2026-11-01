// Builds a standalone player of an imported world and prepares a headless smoke run.
//   Menu:   Ocean > Build Standalone Player (asks for the world's scene)
//   Batch:  Unity -batchmode -projectPath <proj> -executeMethod OceanCompiler.World.Editor.OceanStandaloneBuild.BuildFromCommandLine
//                 -oceanScene Assets/OceanWorld/<name>/<name>.unity -oceanOut <path to .app> -quit
// The scene is saved as a temporary copy that gets an OceanPlayerSmoke component pointing at the world's index; the
// player logs OCEAN_PLAYER_SMOKE and exits (code 0 when every sampled terrain height lies inside its tile's range).
// Run it with:  <player>/Contents/MacOS/<name> -batchmode -nographics -logFile -
using System;
using System.IO;
using UnityEditor;
using UnityEditor.SceneManagement;
using UnityEngine;

namespace OceanCompiler.World.Editor
{
    public static class OceanStandaloneBuild
    {
        const string TempScene = "Assets/_OceanPlayerSmoke.unity";

        [MenuItem("Ocean/Build Standalone Player")]
        public static void BuildFromMenu()
        {
            var scene = EditorUtility.OpenFilePanel("World scene", "Assets", "unity");
            if (string.IsNullOrEmpty(scene)) return;
            var rel = "Assets" + scene.Substring(Application.dataPath.Length);
            var outPath = EditorUtility.SaveFilePanel("Player", "", Path.GetFileNameWithoutExtension(rel), "app");
            if (!string.IsNullOrEmpty(outPath)) Build(rel, outPath);
        }

        public static void BuildFromCommandLine()
        {
            string scene = Arg("-oceanScene"), outPath = Arg("-oceanOut");
            if (scene == null || outPath == null) { Debug.LogError("OCEAN_BUILD_FAIL need -oceanScene and -oceanOut"); EditorApplication.Exit(2); return; }
            EditorApplication.Exit(Build(scene, outPath) ? 0 : 1);
        }

        public static bool Build(string scenePath, string outPath)
        {
            try
            {
                var dir = Path.GetDirectoryName(scenePath).Replace('\\', '/');
                var index = AssetDatabase.LoadAssetAtPath<OceanWorldIndex>(dir + "/Generated/OceanWorldIndex.asset");
                if (index == null) { Debug.LogError("OCEAN_BUILD_FAIL no OceanWorldIndex at " + dir + "/Generated"); return false; }
                var scene = EditorSceneManager.OpenScene(scenePath, OpenSceneMode.Single);
                var go = new GameObject("OceanPlayerSmoke");
                go.AddComponent<OceanPlayerSmoke>().index = index;
                if (!EditorSceneManager.SaveScene(scene, TempScene, true)) { Debug.LogError("OCEAN_BUILD_FAIL cannot save " + TempScene); return false; }
                var report = BuildPipeline.BuildPlayer(new BuildPlayerOptions
                {
                    scenes = new[] { TempScene }, locationPathName = outPath, target = BuildTarget.StandaloneOSX, options = BuildOptions.None,
                });
                bool ok = report.summary.result == UnityEditor.Build.Reporting.BuildResult.Succeeded;
                Debug.Log((ok ? "OCEAN_BUILD_OK " : "OCEAN_BUILD_FAIL ") + outPath + " result=" + report.summary.result + " errors=" + report.summary.totalErrors +
                          " size=" + report.summary.totalSize + " seconds=" + report.summary.totalTime.TotalSeconds.ToString("F0"));
                return ok;
            }
            catch (Exception e) { Debug.LogError("OCEAN_BUILD_FAIL " + e); return false; }
            finally { AssetDatabase.DeleteAsset(TempScene); }
        }

        static string Arg(string name)
        {
            var a = Environment.GetCommandLineArgs();
            for (int i = 0; i < a.Length - 1; i++) if (a[i] == name) return a[i + 1];
            return null;
        }
    }
}
