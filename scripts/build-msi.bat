@echo off
REM Build MSI installer for Pangolin
REM This script creates the MSI installer from an already-built executable

REM The MSI bundles the WebView2 Evergreen bootstrapper (about 2 MB) and runs it
REM when the WebView2 Runtime is missing. Download it once if it is not present.
if not exist ..\dll\MicrosoftEdgeWebview2Setup.exe (
    echo Downloading WebView2 bootstrapper...
    curl.exe -L -o ..\dll\MicrosoftEdgeWebview2Setup.exe https://go.microsoft.com/fwlink/p/?LinkId=2124703 || exit /b 1
)

wix.exe build -arch x64 -ext WixToolset.Util.wixext -define BuildDir=..\build -define ProjectDir=.. -o ..\build\pangolin-amd64.msi ..\pangolin.wxs
