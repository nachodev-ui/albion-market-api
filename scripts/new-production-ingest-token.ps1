[CmdletBinding()]
param(
    [string]$OutputPath = ".\secrets\deployment\ingest-current.token"
)

$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest

$resolvedParent = Split-Path -Parent $OutputPath
if ([string]::IsNullOrWhiteSpace($resolvedParent)) {
    throw "OutputPath debe incluir un directorio."
}

New-Item -ItemType Directory -Path $resolvedParent -Force | Out-Null

$bytes = [System.Security.Cryptography.RandomNumberGenerator]::GetBytes(48)
$token = [Convert]::ToBase64String($bytes).TrimEnd("=").Replace("+", "-").Replace("/", "_")

Set-Content -LiteralPath $OutputPath -Value $token -NoNewline -Encoding utf8

if ($IsWindows -or $env:OS -eq "Windows_NT") {
    $identity = [System.Security.Principal.WindowsIdentity]::GetCurrent().Name
    & icacls.exe $OutputPath /inheritance:r /grant:r "${identity}:F" | Out-Null
    if ($LASTEXITCODE -ne 0) {
        throw "No fue posible restringir los permisos de $OutputPath."
    }
}
else {
    & chmod 600 $OutputPath
    if ($LASTEXITCODE -ne 0) {
        throw "No fue posible aplicar chmod 600 a $OutputPath."
    }
}

$token = $null
[Array]::Clear($bytes, 0, $bytes.Length)

Write-Host "Token de ingesta generado y protegido en: $OutputPath"
Write-Host "No lo subas a Git. Usa este mismo archivo para configurar el receiver colaborador."
