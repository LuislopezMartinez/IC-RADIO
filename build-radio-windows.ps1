$ErrorActionPreference = 'Stop'

$projectRoot = Split-Path -Parent $MyInvocation.MyCommand.Path
$outputPath = Join-Path $projectRoot 'ic-radio.exe'
$wdspPath = Join-Path $projectRoot 'runtime\wdsp.dll'
$fftwPath = Join-Path $projectRoot 'runtime\libfftw3-3.dll'

Write-Host 'Preparando compilacion de IC-RADIO...'

$portableTools = Join-Path (Split-Path -Parent $projectRoot) 'work\toolchain'
$go = Get-Command go.exe -CommandType Application -ErrorAction SilentlyContinue
if ($go) {
    $goPath = $go.Source
} else {
    $goPath = @(
        (Join-Path $portableTools 'go\bin\go.exe'),
        (Join-Path $env:ProgramFiles 'Go\bin\go.exe')
    ) | Where-Object { Test-Path -LiteralPath $_ -PathType Leaf } | Select-Object -First 1
}
if (-not $goPath) {
    throw 'No se encontro go.exe. Instala Go o agrega su carpeta bin al PATH.'
}

$compiler = Get-Command gcc.exe -ErrorAction SilentlyContinue
if (-not $compiler) {
    $knownCompilers = @(
        (Join-Path $portableTools 'w64devkit\bin\gcc.exe'),
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
    throw 'No se encontro gcc.exe. Instala MinGW-w64 o w64devkit para compilar con CGO.'
}
if (-not (Test-Path -LiteralPath $wdspPath) -or -not (Test-Path -LiteralPath $fftwPath)) {
    throw 'Falta el runtime portable WDSP. Ejecuta .\build-wdsp-windows.ps1 primero.'
}

$env:CC = $compiler.FullName
$env:Path = "$(Split-Path -Parent $compiler.FullName);$env:Path"
$env:CGO_ENABLED = '1'

Push-Location $projectRoot
try {
    Write-Host 'Ejecutando pruebas...'
    & $goPath test -mod=vendor -tags noaudio ./...
    if ($LASTEXITCODE -ne 0) { throw "Las pruebas fallaron con codigo $LASTEXITCODE." }

    Write-Host 'Generando ejecutable...'
    & $goPath build -mod=vendor -tags noaudio -trimpath -ldflags '-H=windowsgui' -o $outputPath ./cmd/ic-mic
    if ($LASTEXITCODE -ne 0) { throw "La compilacion fallo con codigo $LASTEXITCODE." }
    & $goPath build -mod=vendor -trimpath -o (Join-Path $projectRoot 'ic-radio-server.exe') ./cmd/ic-radio-server
    if ($LASTEXITCODE -ne 0) { throw "La compilacion del servidor fallo con codigo $LASTEXITCODE." }
}
finally {
    Pop-Location
}

Write-Host "Compilacion completada: $outputPath" -ForegroundColor Green
Write-Host "Runtime WDSP: $wdspPath" -ForegroundColor Green
