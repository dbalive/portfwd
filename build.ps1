$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $MyInvocation.MyCommand.Path
$ver = (Get-Content -Raw (Join-Path $root "VERSION")).Trim()
if (-not $ver) { throw "VERSION is empty" }
# U+7AEF U+53E3 U+8F6C U+53D1 — keep this script ASCII so PS 5.1 does not mojibake the exe name
$base = -join ([char]0x7AEF, [char]0x53E3, [char]0x8F6C, [char]0x53D1)
$out = Join-Path $root ("{0}-v{1}.exe" -f $base, $ver)
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
