.PHONY: build build-debug clean rsrc frontend bindings test help

# Variables
BINARY_NAME=Pangolin
MANIFEST=pangolin.manifest
BUILD_DIR=build
RSRC_SYSO=rsrc.syso
FRONTEND_DIR=ui/frontend
GOOS=windows
GOARCH=amd64

# Default target
all: clean rsrc build

# Build the Windows executable (GUI mode - no console).
# The "production" tag turns off Wails' dev server and devtools.
build: rsrc frontend
	@echo "Building Windows executable (GUI mode)..."
	@mkdir -p $(BUILD_DIR)
	GOOS=$(GOOS) GOARCH=$(GOARCH) go build -tags production -ldflags="-s -w -H windowsgui" -o $(BUILD_DIR)/$(BINARY_NAME).exe
	@echo "Build complete: $(BUILD_DIR)/$(BINARY_NAME).exe"

# Same as build, but keeps Wails devtools (right-click > Inspect) and debug logging
build-debug: rsrc frontend
	@mkdir -p $(BUILD_DIR)
	GOOS=$(GOOS) GOARCH=$(GOARCH) go build -ldflags="-H windowsgui" -o $(BUILD_DIR)/$(BINARY_NAME).exe
	@echo "Debug build complete: $(BUILD_DIR)/$(BINARY_NAME).exe"

# Build the webview frontend into ui/frontend/dist (embedded into the exe)
frontend:
	@echo "Building frontend..."
	cd $(FRONTEND_DIR) && npm ci && npm run build

# Regenerate the TypeScript bindings for the Go services in ui/
bindings:
	GOOS=$(GOOS) GOARCH=$(GOARCH) wails3 generate bindings -ts -i -d $(FRONTEND_DIR)/src/bindings .

# Run the platform-independent UI tests
test:
	go test ./ui/

# Compile the manifest and icon into a .syso. Wails loads the app icon for
# message boxes and toast notifications from icon group resource ID 3, which
# is where its own syso generator puts it (akavel/rsrc used ID 2).
rsrc:
	@echo "Compiling manifest and icon..."
	@go run github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.25 generate syso -manifest $(MANIFEST) -icon icons/icon-orange.ico -arch $(GOARCH) -out $(RSRC_SYSO)
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
	@echo "  make frontend    - Build the webview frontend"
	@echo "  make bindings    - Regenerate TypeScript bindings for the Go services"
	@echo "  make test        - Run UI unit tests"
	@echo "  make rsrc        - Compile the manifest and icon"
	@echo "  make clean       - Remove build/ directory"
	@echo "  make help        - Show this help message"
