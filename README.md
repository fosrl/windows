# Pangolin

A Windows network management application built with Walk (Windows Application Library Kit) for Go. Pangolin provides a system tray interface for network connectivity management.

## Prerequisites

- Go 1.21 or later
- Windows operating system (for running the application)
- Note: You can build on macOS/Linux using cross-compilation (see below)

## Setup

1. Install the Walk dependency:
```bash
go get github.com/tailscale/walk
```

2. Install the rsrc tool (for embedding the manifest):
```bash
go get github.com/akavel/rsrc
```

## Building the Application

### Cross-Compilation from macOS/Linux

Since Walk is Windows-only, you need to cross-compile when building on macOS or Linux:

1. Install the rsrc tool:
```bash
go get github.com/akavel/rsrc
```

2. Compile the manifest:
```bash
go run github.com/akavel/rsrc@latest -manifest pangolin.manifest -o rsrc.syso
```

3. Build for Windows:
```bash
GOOS=windows GOARCH=amd64 go build -ldflags="-H windowsgui" -o Pangolin.exe
```

### Building on Windows

#### Option 1: Embed the manifest as a resource (Recommended)

1. Install the rsrc tool:
```bash
go install github.com/akavel/rsrc@latest
```

2. Compile the manifest:
```bash
rsrc -manifest pangolin.manifest -o rsrc.syso
```

3. Build the application:
```bash
go build -ldflags="-H windowsgui" -o Pangolin.exe
```

#### Option 2: Use external manifest file

1. Rename `pangolin.manifest` to `Pangolin.exe.manifest`
2. Build the application:
```bash
go build -ldflags="-H windowsgui" -o Pangolin.exe
```

**IMPORTANT**: If you don't embed a manifest as a resource, make sure the manifest file is in place before launching the executable. If you launch without it, Windows won't recognize a manifest file you add later - you'll need to rebuild.

## Running

After building, run the executable on Windows:
```bash
Pangolin.exe
```

## Application Features

Pangolin provides:
- System tray icon with context menu
- Login functionality
- Network connection management (Connect/Disconnect)
- Log file management and viewing
- Documentation access

