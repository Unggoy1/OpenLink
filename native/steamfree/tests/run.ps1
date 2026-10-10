# Owned test suite for the Steam-free DLL (built by ..\test.cmd). Runs the DLL
# only inside harness.exe; never launches Halo. Needs dumpbin on PATH (vcvars).
param([Parameter(Mandatory)][string]$Out)
$ErrorActionPreference = 'Stop'
$dll = Join-Path $Out 'steam_api64.dll'
$harness = Join-Path $Out 'harness.exe'
$diagnosticExit = [Convert]::ToUInt32('e0534645', 16) # the DLL's unsupported-call stop

# Export parity: every B002 ordinal/name pair, and nothing else.
$expected = foreach ($line in Get-Content -LiteralPath "$PSScriptRoot\..\exports-b002.tsv") {
    if ($line -eq '' -or $line.StartsWith('#')) { continue }
    $ordinal, $kind, $names = $line -split "`t"
    "$ordinal $names"
}
$actual = foreach ($line in & dumpbin /nologo /exports $dll) {
    if ($line -match '^\s+(\d+)\s+[0-9A-F]+\s+[0-9A-F]{8}\s+(\w+)') { "$($Matches[1]) $($Matches[2])" }
}
if ($LASTEXITCODE -ne 0) { throw 'dumpbin failed' }
$diff = Compare-Object @($expected) @($actual)
if ($diff) { $diff | Format-Table | Out-String | Write-Host; throw 'export table differs from exports-b002.tsv' }
"PASS: all $(@($expected).Count) B002 export names and ordinals"

function Invoke-Harness([string]$Mode, [string[]]$HarnessArgs, [uint32]$ExpectedExit, [string]$Diagnostic) {
    $p = Start-Process -FilePath $harness -ArgumentList (@("`"$dll`"") + $HarnessArgs) -NoNewWindow -Wait -PassThru
    $code = [BitConverter]::ToUInt32([BitConverter]::GetBytes([int]$p.ExitCode), 0)
    if ($code -ne $ExpectedExit) { throw ('{0}: expected exit 0x{1:x}, got 0x{2:x}' -f $Mode, $ExpectedExit, $code) }
    $log = Join-Path $Out "steamfree-$($p.Id).jsonl"
    $events = @(Get-Content -LiteralPath $log | ForEach-Object { $_ | ConvertFrom-Json })
    if (-not $events.Count) { throw "${Mode}: empty diagnostic log" }
    foreach ($e in $events) {
        $fields = ($e.PSObject.Properties.Name | Sort-Object) -join ','
        if ($fields -ne 'event,name,qpc,seq,tid,value') { throw "${Mode}: unexpected log fields $fields" }
    }
    if ($Diagnostic -and -not ($events | Where-Object event -eq $Diagnostic)) { throw "${Mode}: missing $Diagnostic" }
    "PASS: $Mode"
}
Invoke-Harness 'ABI/server' @('-server') 0
Invoke-Harness 'non-server refused' @('nonserver') 0 'init_refused'
Invoke-Harness 'lookalike server flag refused' @('nonserver', '-serverx') 0 'init_refused'
Invoke-Harness 'unsupported interface' @('interface', '-server') $diagnosticExit 'unsupported_interface'
Invoke-Harness 'unsupported export' @('export', '-server') $diagnosticExit 'unsupported_export'
Invoke-Harness 'unsupported slot' @('slot', '-server') $diagnosticExit 'unsupported_slot'
Invoke-Harness 'unsupported async result' @('callresult', '-server') $diagnosticExit 'unsupported_call_result'
'PASS: Steam-free DLL suite'
