$ErrorActionPreference = 'Stop'
$script:TaskName = 'OpenMausBot Private HTTPS Bridge'
$script:RuleName = 'OpenMausBotPrivateHTTPSBridge'

function Get-BridgeConfig([string]$Path) {
    $c = Get-Content -LiteralPath $Path -Raw | ConvertFrom-Json
    $fields = @($c.PSObject.Properties.Name | Sort-Object)
    if (($fields -join ',') -ne 'expected_exe,gateway_ip,listen,public_origin') { throw 'Unexpected config fields' }
    if ($c.listen -notmatch '^(\[[0-9a-fA-F:]+\]|[0-9.]+):18798$') { throw 'Invalid listener' }
    $bindIP = $Matches[1].Trim('[', ']')
    foreach ($ip in @($bindIP, $c.gateway_ip)) {
        $parsed = [Net.IPAddress]::Parse($ip)
        $bytes = $parsed.GetAddressBytes()
        $v4 = $bytes.Length -eq 4 -and $bytes[0] -eq 100 -and $bytes[1] -ge 64 -and $bytes[1] -le 127
        $v6 = $bytes.Length -eq 16 -and (($bytes[0..5] | ForEach-Object { $_.ToString('x2') }) -join '') -eq 'fd7a115ca1e0'
        if (-not ($v4 -or $v6)) { throw 'Only literal tailnet addresses are permitted' }
    }
    if ($c.public_origin -notmatch '^https://[a-zA-Z0-9](?:[a-zA-Z0-9.-]*[a-zA-Z0-9])?/?$') { throw 'Bare HTTPS DNS origin required' }
    $uri = [Uri]$c.public_origin
    $numeric = $null
    if ([Net.IPAddress]::TryParse($uri.Host, [ref]$numeric)) { throw 'DNS hostname required' }
    if ($c.expected_exe -notmatch '^[A-Za-z]:\\') { throw 'Set expected_exe to the genuine absolute local executable path' }
    $c | Add-Member -NotePropertyName BindIP -NotePropertyValue $bindIP
    return $c
}

function Get-BridgeOwner([string]$Root) {
    if (-not [IO.Path]::IsPathRooted($Root) -or $Root -notmatch '^[A-Za-z]:\\') { throw 'Absolute local installation path required' }
    if ((Get-Item -LiteralPath $Root).Attributes -band [IO.FileAttributes]::ReparsePoint) { throw 'Refusing reparse-point root' }
    $owner = Get-Content -LiteralPath (Join-Path $Root 'bridge-owner.json') -Raw | ConvertFrom-Json
    if ($owner.sid -ne [Security.Principal.WindowsIdentity]::GetCurrent().User.Value -or $owner.root -ne [IO.Path]::GetFullPath($Root) -or $owner.id -notmatch '^[0-9a-f-]{36}$') { throw 'Installation ownership mismatch' }
    return $owner
}

function Get-BridgeArguments([string]$Root) {
    return '-config "' + (Join-Path $Root 'bridge.windows.json') + '" -log "' + (Join-Path $Root 'bridge.log') + '"'
}

function Assert-BridgeTask($Task, [string]$Root, $Owner) {
    $actions = @($Task.Actions)
    if ($Task.TaskPath -ne '\' -or $actions.Count -ne 1 -or $actions[0].Execute -ne (Join-Path $Root 'openmausbot-tailnet-bridge.exe') -or $actions[0].Arguments -ne (Get-BridgeArguments $Root) -or $actions[0].WorkingDirectory -ne $Root -or $Task.Description -ne ('Bridge installation ' + $Owner.id)) { throw 'Unexpected task; refusing changes' }
    $principalSID = $Task.Principal.UserId
    if ($principalSID -notmatch '^S-1-') {
        $principalSID = ([Security.Principal.NTAccount]::new($principalSID)).Translate([Security.Principal.SecurityIdentifier]).Value
    }
    if ($principalSID -ne $Owner.sid -or $Task.Principal.RunLevel -ne 'Limited') { throw 'Unexpected task principal' }
}

function Assert-BridgeRule($Rule, [string]$Root, $Owner, $Config) {
    $app = $Rule | Get-NetFirewallApplicationFilter
    $addr = $Rule | Get-NetFirewallAddressFilter
    $port = $Rule | Get-NetFirewallPortFilter
    if ($Rule.Description -ne ('Bridge installation ' + $Owner.id) -or $app.Program -ne (Join-Path $Root 'openmausbot-tailnet-bridge.exe') -or (@($addr.LocalAddress) -join ',') -ne $Config.BindIP -or (@($addr.RemoteAddress) -join ',') -ne $Config.gateway_ip -or $port.LocalPort -ne '18798' -or $port.Protocol -notin @('TCP', '6') -or $Rule.Direction -ne 'Inbound' -or $Rule.Action -ne 'Allow') { throw 'Unexpected firewall rule; refusing changes' }
}

function Stop-BridgeTask([string]$Root) {
    Stop-ScheduledTask -TaskName $script:TaskName -TaskPath '\'
    for ($i = 0; $i -lt 30; $i++) {
        if ((Get-ScheduledTask -TaskName $script:TaskName -TaskPath '\').State -ne 'Running') { break }
        Start-Sleep -Milliseconds 200
    }
    if ((Get-ScheduledTask -TaskName $script:TaskName -TaskPath '\').State -eq 'Running') { throw 'Bridge task did not stop' }
    $running = @(Get-Process -ErrorAction SilentlyContinue | Where-Object { $_.Path -eq (Join-Path $Root 'openmausbot-tailnet-bridge.exe') })
    if ($running.Count) { throw 'A separately launched bridge is still running; close it manually' }
}
