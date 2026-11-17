$ErrorActionPreference = 'Stop'

$projectRoot = Split-Path -Parent $MyInvocation.MyCommand.Path
$runtimeDir = Join-Path $projectRoot 'runtime'
$sourceCommit = 'e69de50d1d702216098019013899ad87534c64e0'
$taskRoot = Join-Path ([System.IO.Path]::GetTempPath()) ("ic-radio-wdsp-" + [guid]::NewGuid().ToString('N'))
$sourceRoot = Join-Path $taskRoot 'source'
$buildRoot = Join-Path $taskRoot 'build'
$mingwRoot = 'C:\msys64\ucrt64'
$compiler = Join-Path $mingwRoot 'bin\gcc.exe'
$fftwDLL = Join-Path $mingwRoot 'bin\libfftw3-3.dll'

if (-not (Test-Path -LiteralPath $compiler)) {
    throw 'WDSP requires the MSYS2 UCRT64 GCC toolchain.'
}
if (-not (Test-Path -LiteralPath $fftwDLL)) {
    throw 'Install mingw-w64-ucrt-x86_64-fftw in MSYS2 before building WDSP.'
}

New-Item -ItemType Directory -Path $taskRoot, $buildRoot, $runtimeDir -Force | Out-Null

try {
    git clone --quiet https://github.com/TAPR/OpenHPSDR-wdsp.git $sourceRoot
    git -C $sourceRoot checkout --quiet $sourceCommit
    $sourceDir = Join-Path $sourceRoot 'wdsp 2.11\Source'
    # The upstream SDR build uses FFTW_PATIENT and expects a large, machine-
    # specific wisdom cache. IC-RADIO uses one fixed 256-frame audio channel;
    # ESTIMATE avoids a multi-minute first start while preserving the DSP math.
    Get-ChildItem -LiteralPath $sourceDir -Filter '*.c' | ForEach-Object {
        $content = Get-Content -LiteralPath $_.FullName -Raw
        if ($content.Contains('FFTW_PATIENT')) {
            [System.IO.File]::WriteAllText($_.FullName, $content.Replace('FFTW_PATIENT', 'FFTW_ESTIMATE'))
        }
    }
    Copy-Item -LiteralPath (Join-Path $projectRoot 'native\wdsp\ic_audio_bridge.c') `
        -Destination (Join-Path $sourceDir 'ic_audio_bridge.c')
    $sources = Get-ChildItem -LiteralPath $sourceDir -Filter '*.c' |
        Where-Object { $_.Name -ne 'snoop.c' } |
        ForEach-Object FullName
    $outputDLL = Join-Path $buildRoot 'wdsp.dll'
    $env:Path = "$(Join-Path $mingwRoot 'bin');$env:Path"

    & $compiler -w -Wno-error=incompatible-pointer-types -O2 -shared `
        -D_CRT_SECURE_NO_WARNINGS -D_USE_MATH_DEFINES `
        -I $sourceDir -I (Join-Path $mingwRoot 'include') `
        $sources -L (Join-Path $mingwRoot 'lib') `
        -lfftw3 -lavrt -lwinmm -lws2_32 -static-libgcc -o $outputDLL
    if ($LASTEXITCODE -ne 0) {
        throw "WDSP compilation failed with exit code $LASTEXITCODE."
    }

    Copy-Item -LiteralPath $outputDLL -Destination (Join-Path $runtimeDir 'wdsp.dll') -Force
    Copy-Item -LiteralPath $fftwDLL -Destination (Join-Path $runtimeDir 'libfftw3-3.dll') -Force
    Write-Host "WDSP 2.11 runtime built in $runtimeDir" -ForegroundColor Green
}
finally {
    $resolvedTemp = [System.IO.Path]::GetFullPath([System.IO.Path]::GetTempPath())
    $resolvedTask = [System.IO.Path]::GetFullPath($taskRoot)
    if ($resolvedTask.StartsWith($resolvedTemp, [System.StringComparison]::OrdinalIgnoreCase) -and
        (Split-Path -Leaf $resolvedTask).StartsWith('ic-radio-wdsp-')) {
        Remove-Item -LiteralPath $resolvedTask -Recurse -Force -ErrorAction SilentlyContinue
    }
}
