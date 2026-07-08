@echo off
REM Build MSI installer for Pangolin
REM This script creates the MSI installer from an already-built executable

wix.exe build -arch x64 -ext WixToolset.Util.wixext -define BuildDir=..\build -define ProjectDir=.. -o ..\build\pangolin-amd64.msi ..\pangolin.wxs

