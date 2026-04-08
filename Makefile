APP_NAME     := wlcsim
VERSION      := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
BUILD_DIR    := build
LDFLAGS      := -s -w -X main.version=$(VERSION)

# ============================================================
# Native build
# ============================================================

.PHONY: build
build:
	go build -ldflags="$(LDFLAGS)" -o $(BUILD_DIR)/wlcsim ./cmd/wlcsim
	go build -ldflags="$(LDFLAGS)" -o $(BUILD_DIR)/wlcsim-console ./cmd/wlcsim-console

.PHONY: run
run: build
	sudo $(BUILD_DIR)/wlcsim -config configs/devices.yaml

.PHONY: run-lan
run-lan: build
	sudo $(BUILD_DIR)/wlcsim -lan -config configs/devices.yaml

# ============================================================
# Cross-compilation for Linux (used by OVA builds)
# ============================================================

.PHONY: build-linux-amd64
build-linux-amd64:
	@mkdir -p $(BUILD_DIR)
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="$(LDFLAGS)" \
		-o $(BUILD_DIR)/wlcsim-linux-amd64 ./cmd/wlcsim
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="$(LDFLAGS)" \
		-o $(BUILD_DIR)/wlcsim-console-linux-amd64 ./cmd/wlcsim-console

.PHONY: build-linux-arm64
build-linux-arm64:
	@mkdir -p $(BUILD_DIR)
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -ldflags="$(LDFLAGS)" \
		-o $(BUILD_DIR)/wlcsim-linux-arm64 ./cmd/wlcsim
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -ldflags="$(LDFLAGS)" \
		-o $(BUILD_DIR)/wlcsim-console-linux-arm64 ./cmd/wlcsim-console

.PHONY: build-linux-all
build-linux-all: build-linux-amd64 build-linux-arm64

# ============================================================
# OVA builds (requires Packer + QEMU)
# ============================================================

.PHONY: ova-amd64
ova-amd64: build-linux-amd64
	cd ova/packer && packer init . && packer build \
		-var "arch=amd64" \
		-var "output_dir=$(CURDIR)/$(BUILD_DIR)" \
		-var "binary_dir=$(CURDIR)/$(BUILD_DIR)" \
		.
	./ova/scripts/package-ova.sh amd64 $(BUILD_DIR)
	@echo "==> $(BUILD_DIR)/wlcsim-amd64.ova"

.PHONY: ova-arm64
ova-arm64: build-linux-arm64
	cd ova/packer && packer init . && packer build \
		-var "arch=arm64" \
		-var "output_dir=$(CURDIR)/$(BUILD_DIR)" \
		-var "binary_dir=$(CURDIR)/$(BUILD_DIR)" \
		.
	./ova/scripts/package-ova.sh arm64 $(BUILD_DIR)
	@echo "==> $(BUILD_DIR)/wlcsim-arm64.ova"

.PHONY: ova-all
ova-all: ova-amd64 ova-arm64
	@echo "Built: $(BUILD_DIR)/wlcsim-amd64.ova $(BUILD_DIR)/wlcsim-arm64.ova"

# ============================================================
# Utilities
# ============================================================

.PHONY: clean
clean:
	rm -rf $(BUILD_DIR)

.PHONY: test-console
test-console: build
	$(BUILD_DIR)/wlcsim-console
