// Lets optional modules (Addressables) subscribe to import completion without the importer referencing them.
using System;
using System.Collections.Generic;
using OceanCompiler.World;

namespace OceanCompiler.World.Editor
{
    public static class OceanWorldImporterHook
    {
        public static void Subscribe(Action<OceanWorldIndex, Dictionary<string, string>> handler) { OceanWorldImporter.TilesImported += handler; }
    }
}
