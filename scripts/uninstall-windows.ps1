param([string]$Root = (Join-Path $env:LOCALAPPDATA 'OpenMausBotTailnetBridge'))
. (Join-Path $PSScriptRoot 'common-windows.ps1')
$owner = Get-BridgeOwner $Root
$c = Get-BridgeConfig (Join-Path $Root 'bridge.windows.json')
$task = Get-ScheduledTask -TaskName $script:TaskName -TaskPath '\' -ErrorAction SilentlyContinue
$rule = Get-NetFirewallRule -Name $script:RuleName -ErrorAction SilentlyContinue
# Validate both resources before removing either.
if ($task) { Assert-BridgeTask $task $Root $owner }
if ($rule) { Assert-BridgeRule $rule $Root $owner $c }
if ($task) {
    Stop-BridgeTask $Root
    Unregister-ScheduledTask -TaskName $script:TaskName -TaskPath '\' -Confirm:$false
}
if ($rule) { Remove-NetFirewallRule -Name $script:RuleName }
Write-Output 'Bridge disabled; standalone files retained. Native app and pairings untouched.'
