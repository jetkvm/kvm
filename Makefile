# --- JetKVM Audio/Toolchain Dev Environment Setup ---
.PHONY: setup_toolchain build_audio_deps dev_env

# Clone the rv1106-system toolchain to $HOME/.jetkvm/rv1106-system
setup_toolchain:
	bash tools/setup_rv1106_toolchain.sh

# Build ALSA and Opus static libs for ARM in $HOME/.jetkvm/audio-libs
build_audio_deps: setup_toolchain
	bash tools/build_audio_deps.sh $(ALSA_VERSION) $(OPUS_VERSION)

# Prepare everything needed for local development (toolchain + audio deps)
dev_env: build_audio_deps
	@echo "Development environment ready."
JETKVM_HOME ?= $(HOME)/.jetkvm
TOOLCHAIN_DIR ?= $(JETKVM_HOME)/rv1106-system
AUDIO_LIBS_DIR ?= $(JETKVM_HOME)/audio-libs
BRANCH    ?= $(shell git rev-parse --abbrev-ref HEAD)
BUILDDATE ?= $(shell date -u +%FT%T%z)
BUILDTS   ?= $(shell date -u +%s)
REVISION  ?= $(shell git rev-parse HEAD)
VERSION_DEV ?= 0.4.7-dev$(shell date +%Y%m%d%H%M)
VERSION ?= 0.4.6

# Audio library versions
ALSA_VERSION ?= 1.2.14
OPUS_VERSION ?= 1.5.2

# Optimization flags for ARM Cortex-A7 with NEON
OPTIM_CFLAGS := -O3 -mcpu=cortex-a7 -mfpu=neon -mfloat-abi=hard -ftree-vectorize -ffast-math -funroll-loops

PROMETHEUS_TAG := github.com/prometheus/common/version
KVM_PKG_NAME := github.com/jetkvm/kvm

GO_BUILD_ARGS := -tags netgo
GO_RELEASE_BUILD_ARGS := -trimpath $(GO_BUILD_ARGS)
GO_LDFLAGS := \
  -s -w \
  -X $(PROMETHEUS_TAG).Branch=$(BRANCH) \
  -X $(PROMETHEUS_TAG).BuildDate=$(BUILDDATE) \
  -X $(PROMETHEUS_TAG).Revision=$(REVISION) \
  -X $(KVM_PKG_NAME).builtTimestamp=$(BUILDTS)

GO_CMD := GOOS=linux GOARCH=arm GOARM=7 go
BIN_DIR := $(shell pwd)/bin

TEST_DIRS := $(shell find . -name "*_test.go" -type f -exec dirname {} \; | sort -u)

hash_resource:
	@shasum -a 256 resource/jetkvm_native | cut -d ' ' -f 1 > resource/jetkvm_native.sha256

build_dev: build_audio_deps hash_resource
	@echo "Building..."
	GOOS=linux GOARCH=arm GOARM=7 \
	CC=$(TOOLCHAIN_DIR)/tools/linux/toolchain/arm-rockchip830-linux-uclibcgnueabihf/bin/arm-rockchip830-linux-uclibcgnueabihf-gcc \
	CGO_ENABLED=1 \
	CGO_CFLAGS="$(OPTIM_CFLAGS) -I$(AUDIO_LIBS_DIR)/alsa-lib-$(ALSA_VERSION)/include -I$(AUDIO_LIBS_DIR)/opus-$(OPUS_VERSION)/include -I$(AUDIO_LIBS_DIR)/opus-$(OPUS_VERSION)/celt" \
	CGO_LDFLAGS="-L$(AUDIO_LIBS_DIR)/alsa-lib-$(ALSA_VERSION)/src/.libs -lasound -L$(AUDIO_LIBS_DIR)/opus-$(OPUS_VERSION)/.libs -lopus -lm -ldl -static" \
	go build \
		-ldflags="$(GO_LDFLAGS) -X $(KVM_PKG_NAME).builtAppVersion=$(VERSION_DEV)" \
		$(GO_RELEASE_BUILD_ARGS) \
		-o $(BIN_DIR)/jetkvm_app cmd/main.go

build_test2json:
	$(GO_CMD) build -o $(BIN_DIR)/test2json cmd/test2json

build_gotestsum:
	@echo "Building gotestsum..."
	$(GO_CMD) install gotest.tools/gotestsum@latest
	cp $(shell $(GO_CMD) env GOPATH)/bin/linux_arm/gotestsum $(BIN_DIR)/gotestsum

build_dev_test: build_audio_deps build_test2json build_gotestsum
# collect all directories that contain tests
	@echo "Building tests for devices ..."
	@rm -rf $(BIN_DIR)/tests && mkdir -p $(BIN_DIR)/tests

	@cat resource/dev_test.sh > $(BIN_DIR)/tests/run_all_tests
	@for test in $(TEST_DIRS); do \
		test_pkg_name=$$(echo $$test | sed 's/^.\///g'); \
		test_pkg_full_name=$(KVM_PKG_NAME)/$$(echo $$test | sed 's/^.\///g'); \
		test_filename=$$(echo $$test_pkg_name | sed 's/\//__/g')_test; \
		GOOS=linux GOARCH=arm GOARM=7 \
		CC=$(TOOLCHAIN_DIR)/tools/linux/toolchain/arm-rockchip830-linux-uclibcgnueabihf/bin/arm-rockchip830-linux-uclibcgnueabihf-gcc \
		CGO_ENABLED=1 \
		CGO_CFLAGS="$(OPTIM_CFLAGS) -I$(AUDIO_LIBS_DIR)/alsa-lib-$(ALSA_VERSION)/include -I$(AUDIO_LIBS_DIR)/opus-$(OPUS_VERSION)/include -I$(AUDIO_LIBS_DIR)/opus-$(OPUS_VERSION)/celt" \
		CGO_LDFLAGS="-L$(AUDIO_LIBS_DIR)/alsa-lib-$(ALSA_VERSION)/src/.libs -lasound -L$(AUDIO_LIBS_DIR)/opus-$(OPUS_VERSION)/.libs -lopus -lm -ldl -static" \
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
	cd ui && npm ci && npm run build:device

dev_release: frontend build_dev
	@echo "Uploading release..."
	@shasum -a 256 bin/jetkvm_app | cut -d ' ' -f 1 > bin/jetkvm_app.sha256
	rclone copyto bin/jetkvm_app r2://jetkvm-update/app/$(VERSION_DEV)/jetkvm_app
	rclone copyto bin/jetkvm_app.sha256 r2://jetkvm-update/app/$(VERSION_DEV)/jetkvm_app.sha256

build_release: frontend build_audio_deps hash_resource
	@echo "Building release..."
	GOOS=linux GOARCH=arm GOARM=7 \
	CC=$(TOOLCHAIN_DIR)/tools/linux/toolchain/arm-rockchip830-linux-uclibcgnueabihf/bin/arm-rockchip830-linux-uclibcgnueabihf-gcc \
	CGO_ENABLED=1 \
	CGO_CFLAGS="$(OPTIM_CFLAGS) -I$(AUDIO_LIBS_DIR)/alsa-lib-$(ALSA_VERSION)/include -I$(AUDIO_LIBS_DIR)/opus-$(OPUS_VERSION)/include -I$(AUDIO_LIBS_DIR)/opus-$(OPUS_VERSION)/celt" \
	CGO_LDFLAGS="-L$(AUDIO_LIBS_DIR)/alsa-lib-$(ALSA_VERSION)/src/.libs -lasound -L$(AUDIO_LIBS_DIR)/opus-$(OPUS_VERSION)/.libs -lopus -lm -ldl -static" \
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
