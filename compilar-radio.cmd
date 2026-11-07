@echo off
setlocal
title Compilar IC-RADIO

cd /d "%~dp0"
echo Compilando IC-RADIO...
echo.

powershell.exe -NoLogo -NoProfile -ExecutionPolicy Bypass -File "%~dp0build-radio-windows.ps1"
set "BUILD_EXIT_CODE=%ERRORLEVEL%"

echo.
if not "%BUILD_EXIT_CODE%"=="0" (
    echo ERROR: La compilacion no se completo correctamente.
) else (
    echo Listo. Se ha generado:
    echo %~dp0ic-radio.exe
)

echo.
pause
exit /b %BUILD_EXIT_CODE%
