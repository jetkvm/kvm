# Build ALSA and Opus static libs for ARM in /opt/jetkvm-audio-libs
build_audio_deps:
	bash .devcontainer/install_audio_deps.sh $(ALSA_VERSION) $(OPUS_VERSION)

# Prepare everything needed for local development (toolchain + audio deps + Go tools)
dev_env: build_audio_deps
	$(CLEAN_GO_CACHE)
	@echo "Installing Go development tools..."
	go install golang.org/x/tools/cmd/goimports@latest
	@echo "Development environment ready."
JETKVM_HOME ?= $(HOME)/.jetkvm
BUILDKIT_PATH ?= /opt/jetkvm-native-buildkit
BUILDKIT_FLAVOR ?= arm-rockchip830-linux-uclibcgnueabihf
AUDIO_LIBS_DIR ?= /opt/jetkvm-audio-libs

BRANCH    ?= $(shell git rev-parse --abbrev-ref HEAD)
BUILDDATE ?= $(shell date -u +%FT%T%z)
BUILDTS   ?= $(shell date -u +%s)
REVISION  ?= $(shell git rev-parse HEAD)
VERSION_DEV := 0.4.9-dev$(shell date +%Y%m%d%H%M)
VERSION := 0.4.8


# Audio library versions
ALSA_VERSION ?= 1.2.14
OPUS_VERSION ?= 1.5.2

# Set PKG_CONFIG_PATH globally for all targets that use CGO with audio libraries
export PKG_CONFIG_PATH := $(AUDIO_LIBS_DIR)/alsa-lib-$(ALSA_VERSION)/utils:$(AUDIO_LIBS_DIR)/opus-$(OPUS_VERSION)

# Common command to clean Go cache with verbose output for all Go builds
CLEAN_GO_CACHE := @echo "Cleaning Go cache..."; go clean -cache -v

# Optimization flags for ARM Cortex-A7 with NEON SIMD
OPTIM_CFLAGS := -O3 -mfpu=neon -mtune=cortex-a7 -mfloat-abi=hard -ftree-vectorize -ffast-math -funroll-loops -mvectorize-with-neon-quad -marm -D__ARM_NEON

# Cross-compilation environment for ARM - exported globally
export GOOS := linux
export GOARCH := arm
export GOARM := 7
export CC := $(BUILDKIT_PATH)/bin/$(BUILDKIT_FLAVOR)-gcc
export CGO_ENABLED := 1
export CGO_CFLAGS := $(OPTIM_CFLAGS) -I$(BUILDKIT_PATH)/$(BUILDKIT_FLAVOR)/include -I$(BUILDKIT_PATH)/$(BUILDKIT_FLAVOR)/sysroot/usr/include
export CGO_LDFLAGS := -L$(BUILDKIT_PATH)/$(BUILDKIT_FLAVOR)/lib -L$(BUILDKIT_PATH)/$(BUILDKIT_FLAVOR)/sysroot/usr/lib -lrockit -lrockchip_mpp -lrga -lpthread -lm -ldl

# Audio-specific flags (only used for audio C binaries, NOT for main Go app)
AUDIO_CFLAGS := $(CGO_CFLAGS) -I$(AUDIO_LIBS_DIR)/alsa-lib-$(ALSA_VERSION)/include -I$(AUDIO_LIBS_DIR)/opus-$(OPUS_VERSION)/include -I$(AUDIO_LIBS_DIR)/opus-$(OPUS_VERSION)/celt
AUDIO_LDFLAGS := $(AUDIO_LIBS_DIR)/alsa-lib-$(ALSA_VERSION)/src/.libs/libasound.a $(AUDIO_LIBS_DIR)/opus-$(OPUS_VERSION)/.libs/libopus.a -lm -ldl -lpthread

PROMETHEUS_TAG := github.com/prometheus/common/version
KVM_PKG_NAME := github.com/jetkvm/kvm

BUILDKIT_FLAVOR := arm-rockchip830-linux-uclibcgnueabihf
BUILDKIT_PATH ?= /opt/jetkvm-native-buildkit
SKIP_NATIVE_IF_EXISTS ?= 0
SKIP_UI_BUILD ?= 0
GO_BUILD_ARGS := -tags netgo,timetzdata,nomsgpack
GO_RELEASE_BUILD_ARGS := -trimpath $(GO_BUILD_ARGS)
GO_LDFLAGS := \
  -s -w \
  -X $(PROMETHEUS_TAG).Branch=$(BRANCH) \
  -X $(PROMETHEUS_TAG).BuildDate=$(BUILDDATE) \
  -X $(PROMETHEUS_TAG).Revision=$(REVISION) \
  -X $(KVM_PKG_NAME).builtTimestamp=$(BUILDTS)

GO_ARGS := GOOS=linux GOARCH=arm GOARM=7 ARCHFLAGS="-arch arm"
# if BUILDKIT_PATH exists, use buildkit to build
ifneq ($(wildcard $(BUILDKIT_PATH)),)
	GO_ARGS := $(GO_ARGS) \
		CGO_CFLAGS="-I$(BUILDKIT_PATH)/$(BUILDKIT_FLAVOR)/include -I$(BUILDKIT_PATH)/$(BUILDKIT_FLAVOR)/sysroot/usr/include" \
		CGO_LDFLAGS="-L$(BUILDKIT_PATH)/$(BUILDKIT_FLAVOR)/lib -L$(BUILDKIT_PATH)/$(BUILDKIT_FLAVOR)/sysroot/usr/lib -lrockit -lrockchip_mpp -lrga -lpthread -lm" \
		CC="$(BUILDKIT_PATH)/bin/$(BUILDKIT_FLAVOR)-gcc" \
		LD="$(BUILDKIT_PATH)/bin/$(BUILDKIT_FLAVOR)-ld" \
		CGO_ENABLED=1 
	# GO_RELEASE_BUILD_ARGS := $(GO_RELEASE_BUILD_ARGS) -x -work
endif

GO_CMD := $(GO_ARGS) go

BIN_DIR := $(shell pwd)/bin

TEST_DIRS := $(shell find . -name "*_test.go" -type f -exec dirname {} \; | sort -u)

build_native:
	@if [ "$(SKIP_NATIVE_IF_EXISTS)" = "1" ] && [ -f "internal/native/cgo/lib/libjknative.a" ]; then \
		echo "libjknative.a already exists, skipping native build..."; \
	else \
		echo "Building native..."; \
			CC="$(BUILDKIT_PATH)/bin/$(BUILDKIT_FLAVOR)-gcc" \
			LD="$(BUILDKIT_PATH)/bin/$(BUILDKIT_FLAVOR)-ld" \
			./scripts/build_cgo.sh; \
	fi

build_dev: build_native build_audio_deps
	$(CLEAN_GO_CACHE)
	@echo "Building..."
	go build \
		-ldflags="$(GO_LDFLAGS) -X $(KVM_PKG_NAME).builtAppVersion=$(VERSION_DEV)" \
		$(GO_RELEASE_BUILD_ARGS) \
		-o $(BIN_DIR)/jetkvm_app -v cmd/main.go

build_test2json:
	$(CLEAN_GO_CACHE)
	$(GO_CMD) build -o $(BIN_DIR)/test2json cmd/test2json

build_gotestsum:
	$(CLEAN_GO_CACHE)
	@echo "Building gotestsum..."
	$(GO_CMD) install gotest.tools/gotestsum@latest
	cp $(shell $(GO_CMD) env GOPATH)/bin/linux_arm/gotestsum $(BIN_DIR)/gotestsum

build_dev_test: build_audio_deps build_test2json build_gotestsum
	$(CLEAN_GO_CACHE)
# collect all directories that contain tests
	@echo "Building tests for devices ..."
	@rm -rf $(BIN_DIR)/tests && mkdir -p $(BIN_DIR)/tests

	@cat resource/dev_test.sh > $(BIN_DIR)/tests/run_all_tests
	@for test in $(TEST_DIRS); do \
		test_pkg_name=$$(echo $$test | sed 's/^.\///g'); \
		test_pkg_full_name=$(KVM_PKG_NAME)/$$(echo $$test | sed 's/^.\///g'); \
		test_filename=$$(echo $$test_pkg_name | sed 's/\//__/g')_test; \
		go test -v \
			-ldflags="$(GO_LDFLAGS) -X $(KVM_PKG_NAME).builtAppVersion=$(VERSION_DEV)" \
			$(GO_BUILD_ARGS) \
			-c -o $(BIN_DIR)/tests/$$test_filename $$test; \
		echo "runTest ./$$test_filename $$test_pkg_full_name" >> $(BIN_DIR)/tests/run_all_tests; \
	done; \
	chmod +x $(BIN_DIR)/tests/run_all_tests; \
	cp $(BIN_DIR)/test2json $(BIN_DIR)/tests/ && chmod +x $(BIN_DIR)/tests/test2json; \
	cp $(BIN_DIR)/gotestsum $(BIN_DIR)/tests/ && chmod +x $(BIN_DIR)/tests/gotestsum; \
	tar czfv device-tests.tar.gz -C $(BIN_DIR)/tests .

frontend:
	@if [ "$(SKIP_UI_BUILD)" = "1" ] && [ -f "static/index.html" ]; then \
		echo "Skipping frontend build..."; \
	else \
		cd ui && npm ci && npm run build:device && \
		find ../static/ -type f \
			\( -name '*.js' \
			-o -name '*.css' \
			-o -name '*.html' \
			-o -name '*.ico' \
			-o -name '*.png' \
			-o -name '*.jpg' \
			-o -name '*.jpeg' \
			-o -name '*.gif' \
			-o -name '*.svg' \
			-o -name '*.webp' \
			-o -name '*.woff2' \
			\) -exec sh -c 'gzip -9 -kfv {}' \; ;\
	fi

dev_release: frontend build_dev
	@echo "Uploading release... $(VERSION_DEV)"
	@shasum -a 256 bin/jetkvm_app | cut -d ' ' -f 1 > bin/jetkvm_app.sha256
	rclone copyto bin/jetkvm_app r2://jetkvm-update/app/$(VERSION_DEV)/jetkvm_app
	rclone copyto bin/jetkvm_app.sha256 r2://jetkvm-update/app/$(VERSION_DEV)/jetkvm_app.sha256

build_release: frontend build_native build_audio_deps
	$(CLEAN_GO_CACHE)
	@echo "Building release..."
	go build \
		-ldflags="$(GO_LDFLAGS) -X $(KVM_PKG_NAME).builtAppVersion=$(VERSION)" \
		$(GO_RELEASE_BUILD_ARGS) \
		-o bin/jetkvm_app cmd/main.go

release:
	@if rclone lsf r2://jetkvm-update/app/$(VERSION)/ | grep -q "jetkvm_app"; then \
		echo "Error: Version $(VERSION) already exists. Please update the VERSION variable."; \
		exit 1; \
	fi
	make build_release
	@echo "Uploading release..."
	@shasum -a 256 bin/jetkvm_app | cut -d ' ' -f 1 > bin/jetkvm_app.sha256
	rclone copyto bin/jetkvm_app r2://jetkvm-update/app/$(VERSION)/jetkvm_app
	rclone copyto bin/jetkvm_app.sha256 r2://jetkvm-update/app/$(VERSION)/jetkvm_app.sha256

# Run both Go and UI linting
lint: lint-go lint-ui
	@echo "All linting completed successfully!"

# Run golangci-lint locally with the same configuration as CI
lint-go: build_audio_deps
	@echo "Running golangci-lint..."
	@mkdir -p static && touch static/.gitkeep
	golangci-lint run --verbose

# Run both Go and UI linting with auto-fix
lint-fix: lint-go-fix lint-ui-fix
	@echo "All linting with auto-fix completed successfully!"

# Run golangci-lint with auto-fix
lint-go-fix: build_audio_deps
	@echo "Running golangci-lint with auto-fix..."
	@mkdir -p static && touch static/.gitkeep
	golangci-lint run --fix --verbose

# Run UI linting locally (mirrors GitHub workflow ui-lint.yml)
lint-ui:
	@echo "Running UI lint..."
	@cd ui && npm ci
	@cd ui && npm run lint

# Run UI linting with auto-fix
lint-ui-fix:
	@echo "Running UI lint with auto-fix..."
	@cd ui && npm ci
	@cd ui && npm run lint:fix

# Legacy alias for UI linting (for backward compatibility)
ui-lint: lint-ui
