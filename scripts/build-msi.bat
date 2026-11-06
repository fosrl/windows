@echo off
REM Build MSI installer for Pangolin
REM This script creates the MSI installer from an already-built executable
REM (make build VERSION=X.Y.Z).
REM Usage: scripts\build-msi.bat X.Y.Z
REM The MSI ProductVersion must be numeric, so any -suffix (e.g. -rc.1) is
REM dropped from the installer version; the file name keeps the full version.

setlocal
cd /d "%~dp0"

if "%~1"=="" (
    echo Usage: %~nx0 ^<version^> 1>&2
    exit /b 1
)
set "VERSION=%~1"
for /f "tokens=1 delims=-" %%v in ("%VERSION%") do set "MSIVERSION=%%v"

REM The MSI bundles the WebView2 Evergreen bootstrapper (about 2 MB) and runs it
REM when the WebView2 Runtime is missing. Download it once if it is not present.
if not exist ..\dll\MicrosoftEdgeWebview2Setup.exe (
    echo Downloading WebView2 bootstrapper...
    curl.exe -L -o ..\dll\MicrosoftEdgeWebview2Setup.exe https://go.microsoft.com/fwlink/p/?LinkId=2124703 || exit /b 1
)

wix.exe build -arch x64 -ext WixToolset.Util.wixext -define Version=%MSIVERSION% -define BuildDir=..\build -define ProjectDir=.. -o ..\build\pangolin-amd64-%VERSION%.msi ..\pangolin.wxs
