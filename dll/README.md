Download the latest version of Wintun from the official repository: [https://www.wintun.net/](https://www.wintun.net/)

Place wintun.dll in this directory when building the installer.

The MSI also bundles the WebView2 Evergreen bootstrapper, `MicrosoftEdgeWebview2Setup.exe`, from [https://go.microsoft.com/fwlink/p/?LinkId=2124703](https://go.microsoft.com/fwlink/p/?LinkId=2124703). `scripts/build-msi.bat` downloads it here when it is missing.
