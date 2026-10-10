# Generates the export definitions and named unsupported-call thunks for the
# Steam-free steam_api64.dll from exports-b002.tsv: exports.def,
# unsupported.asm and generated.h in the output folder.
param([Parameter(Mandatory)][string]$Out)
$ErrorActionPreference = 'Stop'

# Exports with a real implementation in core.cpp; every other export stops the
# server with a named diagnostic.
$supported = @('GetHSteamPipe', 'GetHSteamUser', 'SteamAPI_GetHSteamPipe', 'SteamAPI_GetHSteamUser',
    'SteamAPI_Init', 'SteamAPI_InitSafe', 'SteamAPI_RestartAppIfNecessary', 'SteamAPI_RunCallbacks',
    'SteamAPI_Shutdown', 'SteamAPI_RegisterCallback', 'SteamAPI_UnregisterCallback',
    'SteamAPI_RegisterCallResult', 'SteamAPI_UnregisterCallResult', 'SteamInternal_ContextInit',
    'SteamInternal_FindOrCreateUserInterface')
# Implemented interface slots (vtable index -> function in core.cpp) for
# SteamUtils009, STEAMAPPS_INTERFACE_VERSION008 and STEAMHTMLSURFACE_INTERFACE_VERSION_005.
$methods = @(
    @{ 16 = 'UtilsWarning' },
    @{ 4 = 'AppsLanguage'; 6 = 'AppsSubscribed'; 21 = 'AppsQuery'; 26 = 'AppsLaunch' },
    @{ 2 = 'HtmlShutdown' })
$slots = 160

$rows = foreach ($line in Get-Content -LiteralPath "$PSScriptRoot\exports-b002.tsv") {
    if ($line -eq '' -or $line.StartsWith('#')) { continue }
    $ordinal, $kind, $names = $line -split "`t"
    [pscustomobject]@{ Ordinal = [int]$ordinal; Kind = $kind; Names = @($names -split ',' | Where-Object { $_ }) }
}
if ($rows.Count -ne 995) { throw "expected 995 B002 exports, found $($rows.Count)" }

$def = [Collections.Generic.List[string]]@('LIBRARY steam_api64.dll', 'EXPORTS')
$asm = [Collections.Generic.List[string]]@('EXTERN UnsupportedExport:PROC', 'EXTERN UnsupportedSlot:PROC', '_TEXT SEGMENT')
$header = [Collections.Generic.List[string]]@('#pragma once')
for ($i = 0; $i -lt $rows.Count; $i++) {
    $row = $rows[$i]
    if ($row.Kind -eq 'data') {
        if (($row.Names -join ',') -ne 'g_pSteamClientGameServer') { throw 'unverified data export' }
        $def.Add("    g_pSteamClientGameServer @$($row.Ordinal) DATA")
        continue
    }
    $implemented = [bool]($row.Names | Where-Object { $_ -in $supported })
    $label = if ($implemented) { $row.Names[0] } else { "Unsupported$i" }
    if ($row.Names.Count) {
        foreach ($name in $row.Names) { $def.Add("    $name=$label @$($row.Ordinal)") }
    } else {
        $def.Add("    $label=$label @$($row.Ordinal) NONAME")
    }
    if (-not $implemented) {
        $asm.AddRange([string[]]@("PUBLIC $label", "$label PROC", "mov ecx, $i", 'jmp UnsupportedExport', "$label ENDP"))
    }
}
for ($group = 0; $group -lt $methods.Count; $group++) {
    for ($slot = 0; $slot -lt $slots; $slot++) {
        if ($methods[$group].ContainsKey($slot)) { continue }
        $name = "Slot_${group}_$slot"
        $asm.AddRange([string[]]@("PUBLIC $name", "$name PROC", "mov edx, $group", "mov r8d, $slot",
                'jmp UnsupportedSlot', "$name ENDP"))
        $header.Add("extern `"C`" void $name();")
    }
}
$asm.AddRange([string[]]@('_TEXT ENDS', 'END'))
$exportNames = foreach ($row in $rows) { '"' + $(if ($row.Names.Count) { $row.Names -join '|' } else { "$($row.Ordinal)" }) + '"' }
$header.Add('static const char* const ExportNames[] = {' + ($exportNames -join ',') + '};')
for ($group = 0; $group -lt $methods.Count; $group++) {
    $entries = for ($slot = 0; $slot -lt $slots; $slot++) {
        $target = if ($methods[$group].ContainsKey($slot)) { $methods[$group][$slot] } else { "Slot_${group}_$slot" }
        "reinterpret_cast<void*>($target)"
    }
    $header.Add("static void* const Table$group[] = {" + ($entries -join ',') + '};')
}

New-Item -ItemType Directory -Force $Out | Out-Null
[IO.File]::WriteAllText("$Out\exports.def", ($def -join "`n") + "`n")
[IO.File]::WriteAllText("$Out\unsupported.asm", ($asm -join "`n") + "`n")
[IO.File]::WriteAllText("$Out\generated.h", ($header -join "`n") + "`n")
"Generated $($rows.Count) B002 exports; $($supported.Count) typed API names, $($methods.Count) typed interface tables"
