$ErrorActionPreference = 'Stop'

$projectRoot = Split-Path -Parent $MyInvocation.MyCommand.Path
$compiler = Get-Command gcc.exe -ErrorAction SilentlyContinue
if (-not $compiler) {
    throw 'No se encontró gcc.exe en PATH. Instala MinGW-w64 o w64devkit para compilar con CGO.'
}

$env:CC = $compiler.Source
$env:CGO_ENABLED = '1'

Push-Location $projectRoot
try {
    go test -mod=vendor -tags noaudio ./...
    go build -mod=vendor -tags noaudio -trimpath -ldflags '-H=windowsgui' -o ic-radio.exe ./cmd/ic-mic
}
finally {
    Pop-Location
}

Write-Host "Compilación completada: $(Join-Path $projectRoot 'ic-radio.exe')"
