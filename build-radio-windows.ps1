$ErrorActionPreference = 'Stop'

$projectRoot = Split-Path -Parent $MyInvocation.MyCommand.Path
$compiler = Get-Command gcc.exe -ErrorAction SilentlyContinue
if (-not $compiler) {
    $knownCompilers = @(
        'C:\msys64\ucrt64\bin\gcc.exe',
        'C:\msys64\mingw64\bin\gcc.exe',
        'C:\mingw64\bin\gcc.exe',
        'C:\w64devkit\bin\gcc.exe'
    )
    $compilerPath = $knownCompilers | Where-Object { Test-Path -LiteralPath $_ } | Select-Object -First 1
    if ($compilerPath) {
        $compiler = Get-Item -LiteralPath $compilerPath
    }
}
if (-not $compiler) {
    throw 'No se encontró gcc.exe en PATH. Instala MinGW-w64 o w64devkit para compilar con CGO.'
}

$env:CC = $compiler.FullName
$env:Path = "$(Split-Path -Parent $compiler.FullName);$env:Path"
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
