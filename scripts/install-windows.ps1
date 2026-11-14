param([string]$Root = (Join-Path $env:LOCALAPPDATA 'OpenMausBotTailnetBridge'))
. (Join-Path $PSScriptRoot 'common-windows.ps1')
$owner = Get-BridgeOwner $Root
$c = Get-BridgeConfig (Join-Path $Root 'bridge.windows.json')
$exe = Join-Path $Root 'openmausbot-tailnet-bridge.exe'
if (-not (Test-Path -LiteralPath $exe -PathType Leaf)) { throw 'Missing staged executable' }
if (Get-ScheduledTask -TaskName $script:TaskName -ErrorAction SilentlyContinue) { throw 'Task name exists; refusing overwrite' }
if (Get-NetFirewallRule -Name $script:RuleName -ErrorAction SilentlyContinue) { throw 'Firewall rule exists; refusing overwrite' }
$madeRule = $false
$madeTask = $false
try {
    New-NetFirewallRule -Name $script:RuleName -DisplayName 'OpenMausBot private bridge (gateway only)' -Description ('Bridge installation ' + $owner.id) -Direction Inbound -Action Allow -Protocol TCP -LocalAddress $c.BindIP -LocalPort 18798 -RemoteAddress $c.gateway_ip -Program $exe -Profile Any | Out-Null
    $madeRule = $true
    $action = New-ScheduledTaskAction -Execute $exe -Argument (Get-BridgeArguments $Root) -WorkingDirectory $Root
    $user = [Security.Principal.WindowsIdentity]::GetCurrent().Name
    $trigger = New-ScheduledTaskTrigger -AtLogOn -User $user
    $principal = New-ScheduledTaskPrincipal -UserId $user -LogonType Interactive -RunLevel Limited
    $settings = New-ScheduledTaskSettingsSet -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries -ExecutionTimeLimit ([TimeSpan]::Zero) -RestartCount 3 -RestartInterval (New-TimeSpan -Minutes 1) -Hidden
    Register-ScheduledTask -TaskName $script:TaskName -TaskPath '\' -Action $action -Trigger $trigger -Principal $principal -Settings $settings -Description ('Bridge installation ' + $owner.id) | Out-Null
    $madeTask = $true
    Start-ScheduledTask -TaskName $script:TaskName -TaskPath '\'
    Write-Output 'Installed limited interactive task and gateway-only firewall rule.'
} catch {
    if ($madeTask) {
        Stop-ScheduledTask -TaskName $script:TaskName -TaskPath '\' -ErrorAction SilentlyContinue
        Unregister-ScheduledTask -TaskName $script:TaskName -TaskPath '\' -Confirm:$false
    }
    if ($madeRule) { Remove-NetFirewallRule -Name $script:RuleName }
    throw
}
