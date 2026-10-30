// Registers imported tiles with Addressables. Compiled only when com.unity.addressables is installed
// (OCEAN_ADDRESSABLES is defined by the asmdef). The game keeps its own groups and build settings:
// this only creates one group per world if missing and puts each tile prefab and the index in it.
//   address  ocean/<world>/<tile id>   (the same string is stored in OceanTileEntry.address)
//   address  ocean/<world>/index       the OceanWorldIndex asset
//   labels   ocean-tile, ocean-<world>  (index: ocean-index, ocean-<world>)
using System.Collections.Generic;
using OceanCompiler.World;
using UnityEditor;
using UnityEditor.AddressableAssets;
using UnityEditor.AddressableAssets.Settings;
using UnityEditor.AddressableAssets.Settings.GroupSchemas;

namespace OceanCompiler.World.Editor
{
    static class OceanAddressables
    {
        [InitializeOnLoadMethod]
        static void Hook() { OceanWorldImporterHook.Subscribe(Register); }

        static void Register(OceanWorldIndex index, Dictionary<string, string> prefabs)
        {
            var settings = AddressableAssetSettingsDefaultObject.GetSettings(true);
            string groupName = "Ocean_" + index.worldName;
            var group = settings.FindGroup(groupName)
                ?? settings.CreateGroup(groupName, false, false, true, null, typeof(BundledAssetGroupSchema), typeof(ContentUpdateGroupSchema));
            string worldLabel = "ocean-" + index.worldName;
            Add(settings, group, AssetDatabase.GetAssetPath(index), "ocean/" + index.worldName + "/index", "ocean-index", worldLabel);
            foreach (var kv in prefabs)
                Add(settings, group, kv.Value, "ocean/" + index.worldName + "/" + kv.Key, "ocean-tile", worldLabel);
            AssetDatabase.SaveAssets();
            UnityEngine.Debug.Log("OCEAN_ADDRESSABLES group=" + groupName + " entries=" + (prefabs.Count + 1));
        }

        /// <summary>Builds Addressables content and checks every tile address of every imported world resolves to a prefab with a Terrain.
        /// Batchmode: -executeMethod OceanCompiler.World.Editor.OceanAddressables.BuildAndCheckFromCommandLine</summary>
        [MenuItem("Ocean/Addressables/Build And Check")]
        public static void BuildAndCheck()
        {
            var settings = AddressableAssetSettingsDefaultObject.GetSettings(false);
            if (settings == null) throw new System.Exception("no Addressables settings: import a world first");
            AddressableAssetSettings.BuildPlayerContent(out var result);
            if (!string.IsNullOrEmpty(result.Error)) throw new System.Exception("Addressables build failed: " + result.Error);
            int tiles = 0;
            foreach (var guid in AssetDatabase.FindAssets("t:OceanWorldIndex"))
            {
                var index = AssetDatabase.LoadAssetAtPath<OceanWorldIndex>(AssetDatabase.GUIDToAssetPath(guid));
                foreach (var t in index.tiles)
                {
                    var entry = settings.FindAssetEntry(AssetDatabase.AssetPathToGUID("Assets/OceanWorld/" + index.worldName + "/Generated/Prefabs/" + t.id + ".prefab"));
                    if (entry == null || entry.address != t.address) throw new System.Exception("tile " + t.id + " is not addressable at " + t.address);
                    if (!entry.labels.Contains("ocean-tile") || !entry.labels.Contains("ocean-" + index.worldName)) throw new System.Exception("tile " + t.id + " lacks labels");
                    if (!(entry.MainAsset is UnityEngine.GameObject prefab) || prefab.GetComponent<UnityEngine.Terrain>() == null) throw new System.Exception("tile " + t.id + " prefab has no Terrain");
                    tiles++;
                }
            }
            // Make play mode load the bundles just built, so runtime tests exercise real Addressables loading.
            for (int i = 0; i < settings.DataBuilders.Count; i++)
                if (settings.DataBuilders[i].GetType().Name == "BuildScriptPackedPlayMode") settings.ActivePlayModeDataBuilderIndex = i;
            EditorUtility.SetDirty(settings);
            AssetDatabase.SaveAssets();
            UnityEngine.Debug.Log("OCEAN_ADDRESSABLES_OK tiles=" + tiles + " bundles built, addresses and labels verified");
        }

        public static void BuildAndCheckFromCommandLine()
        {
            try { BuildAndCheck(); }
            catch (System.Exception e) { UnityEngine.Debug.LogError("Ocean addressables check failed: " + e); EditorApplication.Exit(1); }
        }

        static void Add(AddressableAssetSettings settings, AddressableAssetGroup group, string path, string address, params string[] labels)
        {
            var entry = settings.CreateOrMoveEntry(AssetDatabase.AssetPathToGUID(path), group, false, false);
            entry.address = address;
            foreach (var l in labels) entry.SetLabel(l, true, true, false);
        }
    }
}
