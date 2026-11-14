$ErrorActionPreference = 'Stop'
$failed = $false
Get-ChildItem -LiteralPath $PSScriptRoot -Filter '*.ps1' | ForEach-Object {
    $tokens = $null
    $errors = $null
    $null = [Management.Automation.Language.Parser]::ParseFile($_.FullName, [ref]$tokens, [ref]$errors)
    if ($errors.Count) {
        $errors | ForEach-Object { Write-Error $_.Message -ErrorAction Continue }
        $failed = $true
    }
}
if ($failed) { exit 1 }
Write-Output 'PowerShell syntax OK'
