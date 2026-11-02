.PHONY: build build-debug build-linux clean rsrc frontend bindings test help

# Variables
BINARY_NAME=Pangolin
MANIFEST=pangolin.manifest
BUILD_DIR=build
RSRC_SYSO=rsrc.syso
FRONTEND_DIR=ui/frontend
GOOS=windows
GOARCH=amd64
LINUX_BINARY_NAME=pangolin-client
LINUX_GOARCH ?= $(shell go env GOHOSTARCH)
OLM_VERSION := $(shell go list -m -f '{{.Version}}' github.com/fosrl/olm | sed 's/^v//')
OLM_LDFLAGS = -X github.com/fosrl/windows/version.OlmVersionOverride=$(OLM_VERSION)

# Default target
all: clean rsrc build

# Build the Windows executable (GUI mode - no console).
# The "production" tag turns off Wails' dev server and devtools.
build: rsrc frontend
	@echo "Building Windows executable (GUI mode)..."
	@mkdir -p $(BUILD_DIR)
	GOOS=$(GOOS) GOARCH=$(GOARCH) go build -tags production -ldflags="-s -w -H windowsgui $(OLM_LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME).exe
	@echo "Build complete: $(BUILD_DIR)/$(BINARY_NAME).exe"

# Same as build, but keeps Wails devtools (right-click > Inspect) and debug logging
build-debug: rsrc frontend
	@mkdir -p $(BUILD_DIR)
	GOOS=$(GOOS) GOARCH=$(GOARCH) go build -ldflags="-H windowsgui $(OLM_LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME).exe
	@echo "Debug build complete: $(BUILD_DIR)/$(BINARY_NAME).exe"

# Build the Linux binary. Needs cgo, gtk3 and webkit2gtk-4.1: the UI uses
# Wails' GTK3 stack (the gtk3 tag) because GTK4 can't position the tray popup.
build-linux: frontend
	@echo "Building Linux binary..."
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=1 GOOS=linux GOARCH=$(LINUX_GOARCH) go build -tags "gtk3 production" -ldflags="-s -w $(OLM_LDFLAGS)" -o $(BUILD_DIR)/$(LINUX_BINARY_NAME)
	@echo "Build complete: $(BUILD_DIR)/$(LINUX_BINARY_NAME)"

# Build the webview frontend into ui/frontend/dist (embedded into the exe)
frontend:
	@echo "Building frontend..."
	cd $(FRONTEND_DIR) && npm ci && npm run build

# Regenerate the TypeScript bindings for the Go services in ui/
bindings:
	GOOS=$(GOOS) GOARCH=$(GOARCH) wails3 generate bindings -ts -i -d $(FRONTEND_DIR)/src/bindings .

# Run the tests for this platform (the gtk3 tag only matters on Linux)
test:
	go test -tags gtk3 ./...

# Compile the manifest and icon into a .syso. Wails loads the app icon for
# message boxes and toast notifications from icon group resource ID 3, which
# is where its own syso generator puts it (akavel/rsrc used ID 2).
rsrc:
	@echo "Compiling manifest and icon..."
	@go run github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0 generate syso -manifest $(MANIFEST) -icon icons/icon-orange.ico -arch $(GOARCH) -out $(RSRC_SYSO)
	@echo "Resources compiled: $(RSRC_SYSO)"

# Clean build artifacts
clean:
	@echo "Cleaning build artifacts..."
	@rm -rf $(BUILD_DIR)
	@rm -f $(RSRC_SYSO)
	@echo "Clean complete"

# Show help
help:
	@echo "Available targets:"
	@echo "  make build       - Build the Windows executable to build/ (GUI mode, no console)"
	@echo "  make build-debug - Build with Wails devtools enabled"
	@echo "  make build-linux - Build the Linux binary to build/ (needs gtk3, webkit2gtk-4.1)"
	@echo "  make frontend    - Build the webview frontend"
	@echo "  make bindings    - Regenerate TypeScript bindings for the Go services"
	@echo "  make test        - Run unit tests"
	@echo "  make rsrc        - Compile the manifest and icon"
	@echo "  make clean       - Remove build/ directory"
	@echo "  make help        - Show this help message"
