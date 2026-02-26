.PHONY: build build-debug clean rsrc help

# Variables
BINARY_NAME=Pangolin
MANIFEST=pangolin.manifest
BUILD_DIR=build
RSRC_SYSO=rsrc.syso
GOOS=windows
GOARCH=amd64

# Default target
all: clean rsrc build

# Build the Windows executable (GUI mode - no console)
build: rsrc
	@echo "Building Windows executable (GUI mode)..."
	@mkdir -p $(BUILD_DIR)
	GOOS=$(GOOS) GOARCH=$(GOARCH) go build -ldflags="-s -w -H windowsgui" -o $(BUILD_DIR)/$(BINARY_NAME).exe
	@echo "Build complete: $(BUILD_DIR)/$(BINARY_NAME).exe"


# Compile the manifest and icons using rsrc
rsrc:
	@echo "Compiling manifest..."
	@go run github.com/akavel/rsrc@latest -manifest $(MANIFEST) -ico icons/icon-orange.ico -o $(RSRC_SYSO)
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
	@echo "  make rsrc        - Compile the manifest file"
	@echo "  make clean       - Remove build/ directory"
	@echo "  make help        - Show this help message"

