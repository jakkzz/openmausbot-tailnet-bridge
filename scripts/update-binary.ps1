param(
    [Parameter(Mandatory=$true)][string]$NewBinary,
    [Parameter(Mandatory=$true)][ValidatePattern('^[A-Fa-f0-9]{64}$')][string]$ExpectedSHA256,
    [string]$Root = (Join-Path $env:LOCALAPPDATA 'OpenMausBotTailnetBridge')
)
. (Join-Path $PSScriptRoot 'common-windows.ps1')
$owner = Get-BridgeOwner $Root
$task = Get-ScheduledTask -TaskName $script:TaskName -TaskPath '\'
Assert-BridgeTask $task $Root $owner
$exe = Join-Path $Root 'openmausbot-tailnet-bridge.exe'
$new = Join-Path $Root 'openmausbot-tailnet-bridge.candidate.exe'
$backup = $exe + '.previous'
if ((Test-Path -LiteralPath $new) -or (Test-Path -LiteralPath $backup)) { throw 'Staging/backup exists; inspect and archive it manually first' }
if ((Get-FileHash -Algorithm SHA256 -LiteralPath $NewBinary).Hash -ne $ExpectedSHA256) { throw 'New binary hash mismatch' }
Copy-Item -LiteralPath $NewBinary -Destination $new
if ((Get-FileHash -Algorithm SHA256 -LiteralPath $new).Hash -ne $ExpectedSHA256) { throw 'Copied binary hash mismatch; task left unchanged' }
# Probe requires the genuine app and owner phone-access UI to be awake.
$probeArgs = '-probe -config "' + (Join-Path $Root 'bridge.windows.json') + '"'
$probe = Start-Process -FilePath $new -ArgumentList $probeArgs -Wait -PassThru
if ($probe.ExitCode -ne 0) { throw 'Candidate probe failed; task left unchanged; inspect candidate.exe' }
Stop-BridgeTask $Root
try {
    Move-Item -LiteralPath $exe -Destination $backup
} catch {
    Start-ScheduledTask -TaskName $script:TaskName -TaskPath '\'
    throw
}
try {
    Move-Item -LiteralPath $new -Destination $exe
    Start-ScheduledTask -TaskName $script:TaskName -TaskPath '\'
    Start-Sleep -Seconds 1
    if ((Get-ScheduledTask -TaskName $script:TaskName -TaskPath '\').State -ne 'Running') { throw 'Updated task exited' }
    Write-Output 'Standalone bridge updated; backup retained. Native app untouched.'
} catch {
    Stop-BridgeTask $Root
    if (Test-Path -LiteralPath $exe) { Move-Item -LiteralPath $exe -Destination $new }
    Move-Item -LiteralPath $backup -Destination $exe
    Start-ScheduledTask -TaskName $script:TaskName -TaskPath '\'
    throw
}
