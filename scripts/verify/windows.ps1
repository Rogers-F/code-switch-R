$ErrorActionPreference = "Stop"

Write-Host "Code-Switch-R verification (Windows)"
Write-Host ("Time: {0}" -f (Get-Date).ToString("o"))
Write-Host ""

$hostsPath = Join-Path $env:SystemRoot "System32\drivers\etc\hosts"
$markerStart = "# === Code-Switch MITM Start ==="
$markerEnd = "# === Code-Switch MITM End ==="
$certName = "Code-Switch MITM CA"
$logDir = Join-Path $env:USERPROFILE ".code-switch\logs"
$dbPath = Join-Path $env:USERPROFILE ".code-switch\app.db"
$certDir = Join-Path $env:USERPROFILE ".code-switch\certs"

Write-Host "[1] Hosts"
if (Test-Path $hostsPath) {
  $content = Get-Content -LiteralPath $hostsPath -ErrorAction Stop
  if ($content -contains $markerStart) {
    Write-Host "  - marker: FOUND ($markerStart ... $markerEnd)"
  } else {
    Write-Host "  - marker: NOT FOUND"
  }
} else {
  Write-Host "  - hosts file not found: $hostsPath"
}
Write-Host ""

Write-Host "[2] Root CA"
try {
  $out = & certutil.exe -store "ROOT" 2>$null
  if ($out -match [Regex]::Escape($certName)) {
    Write-Host "  - installed: YES ($certName)"
  } else {
    Write-Host "  - installed: NO ($certName)"
  }
} catch {
  Write-Host "  - certutil.exe failed, run PowerShell as Administrator for full details"
}
Write-Host ""

Write-Host "[3] Port 443"
try {
  $listening = netstat -ano | Select-String -Pattern "LISTENING\s+0\.0\.0\.0:443|LISTENING\s+\[::\]:443"
  if ($listening) {
    Write-Host "  - listening: YES"
    $listening | ForEach-Object { Write-Host ("  - " + $_.Line) }
  } else {
    Write-Host "  - listening: NO"
  }
} catch {
  Write-Host "  - netstat failed"
}
Write-Host ""

Write-Host "[4] App data"
Write-Host ("  - db: {0} ({1})" -f $dbPath, (Test-Path $dbPath))
Write-Host ("  - cert dir: {0} ({1})" -f $certDir, (Test-Path $certDir))
Write-Host ("  - log dir: {0} ({1})" -f $logDir, (Test-Path $logDir))

