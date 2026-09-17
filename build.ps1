$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $MyInvocation.MyCommand.Path
$ver = (Get-Content -Raw (Join-Path $root "VERSION")).Trim()
if (-not $ver) { throw "VERSION is empty" }
$out = Join-Path $root ("PortFwd-v{0}.exe" -f $ver)
$env:GOPROXY = "https://goproxy.cn,direct"
Set-Location $root
$rsrc = Join-Path (& "C:\Program Files\Go\bin\go.exe" env GOPATH) "bin\rsrc.exe"
$ico = Join-Path $root "cmd\portfwd\web\icon.ico"
$syso = Join-Path $root "cmd\portfwd\rsrc_windows_amd64.syso"
if ((Test-Path -LiteralPath $rsrc) -and (Test-Path -LiteralPath $ico)) {
  & $rsrc -arch amd64 -ico $ico -o $syso
  if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
}
& "C:\Program Files\Go\bin\go.exe" build -ldflags="-H windowsgui -s -w" -o $out ./cmd/portfwd
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
Get-Item -LiteralPath $out | Format-List FullName, Length, LastWriteTime
