param(
    [Parameter(Mandatory=$true)][string]$Binary,
    [Parameter(Mandatory=$true)][string]$Config,
    [Parameter(Mandatory=$true)][ValidatePattern('^[A-Fa-f0-9]{64}$')][string]$ExpectedSHA256,
    [string]$Root = (Join-Path $env:LOCALAPPDATA 'OpenMausBotTailnetBridge')
)
. (Join-Path $PSScriptRoot 'common-windows.ps1')
if ($Root -notmatch '^[A-Za-z]:\\' -or $Root.Contains('"')) { throw 'Absolute local path required' }
$Root = [IO.Path]::GetFullPath($Root)
if (Test-Path -LiteralPath $Root) { throw 'Destination exists; refusing overwrite' }
$Binary = (Resolve-Path -LiteralPath $Binary).Path
$Config = (Resolve-Path -LiteralPath $Config).Path
$c = Get-BridgeConfig $Config
if (-not (Test-Path -LiteralPath $c.expected_exe -PathType Leaf)) { throw 'Genuine native executable is missing' }
if ((Get-FileHash -Algorithm SHA256 -LiteralPath $Binary).Hash -ne $ExpectedSHA256) { throw 'Binary hash mismatch' }
# Never inherit a permissive ACL onto installed contents.
New-Item -ItemType Directory -Path $Root | Out-Null
$user = [Security.Principal.WindowsIdentity]::GetCurrent().User
$acl = [Security.AccessControl.DirectorySecurity]::new()
$acl.SetOwner($user)
$acl.SetAccessRuleProtection($true, $false)
foreach ($who in @($user, [Security.Principal.SecurityIdentifier]::new('S-1-5-18'))) {
    $acl.AddAccessRule([Security.AccessControl.FileSystemAccessRule]::new($who, 'FullControl', 'ContainerInherit,ObjectInherit', 'None', 'Allow'))
}
Set-Acl -LiteralPath $Root -AclObject $acl
Copy-Item -LiteralPath $Binary -Destination (Join-Path $Root 'openmausbot-tailnet-bridge.exe')
Copy-Item -LiteralPath $Config -Destination (Join-Path $Root 'bridge.windows.json')
@{sid=$user.Value; root=$Root; id=[Guid]::NewGuid().ToString()} | ConvertTo-Json | Set-Content -LiteralPath (Join-Path $Root 'bridge-owner.json') -Encoding UTF8
Write-Output 'Created private standalone installation. Native application untouched.'
