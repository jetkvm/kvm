BRANCH    ?= $(shell git rev-parse --abbrev-ref HEAD 2>/dev/null || echo unknown)
BUILDDATE := $(shell date -u +%FT%T%z)
BUILDTS   := $(shell date -u +%s)
REVISION  ?= $(shell git rev-parse HEAD 2>/dev/null || echo unknown)
VERSION := 0.5.3
VERSION_DEV := $(VERSION)-dev$(shell date -u +%Y%m%d%H%M)

PROMETHEUS_TAG := github.com/prometheus/common/version
KVM_PKG_NAME := github.com/jetkvm/kvm

BUILDKIT_FLAVOR := arm-rockchip830-linux-uclibcgnueabihf
BUILDKIT_PATH ?= /opt/jetkvm-native-buildkit
DOCKER_BUILD_TAG ?= ghcr.io/jetkvm/buildkit:latest
SKIP_NATIVE_IF_EXISTS ?= 0
SKIP_UI_BUILD ?= 0
ENABLE_SYNC_TRACE ?= 0
TARGET_PLATFORM ?= jetkvm

# Keep JetKVM developer workflows identical by default (CGO + native libs on).
# Other platforms default to pure-Go builds until their native HAL backends are added.
ifeq ($(TARGET_PLATFORM),jetkvm)
	ENABLE_JETKVM_NATIVE_CGO ?= 1
	ENABLE_JETKVM_AUDIO_CGO ?= 1
	CGO_ENABLED ?= 1
else
	ENABLE_JETKVM_NATIVE_CGO ?= 0
	ENABLE_JETKVM_AUDIO_CGO ?= 0
	CGO_ENABLED ?= 0
endif

SUPPORTED_PLATFORMS := jetkvm comet-pro nanokvm-pro
PLATFORM_jetkvm_ARCH := arm
PLATFORM_jetkvm_GOARM := 7
PLATFORM_comet-pro_ARCH := arm64
PLATFORM_nanokvm-pro_ARCH := arm64

TARGET_ARCH := $(PLATFORM_$(TARGET_PLATFORM)_ARCH)
TARGET_GOARM := $(PLATFORM_$(TARGET_PLATFORM)_GOARM)

ifeq ($(strip $(TARGET_ARCH)),)
$(error Unsupported TARGET_PLATFORM '$(TARGET_PLATFORM)'; supported values: $(SUPPORTED_PLATFORMS))
endif

CMAKE_BUILD_TYPE ?= Release

ifneq ($(ENABLE_JETKVM_NATIVE_CGO),0)
ifneq ($(TARGET_PLATFORM),jetkvm)
$(error ENABLE_JETKVM_NATIVE_CGO requires TARGET_PLATFORM=jetkvm)
endif
ifeq ($(CGO_ENABLED),0)
$(error ENABLE_JETKVM_NATIVE_CGO requires CGO_ENABLED=1)
endif
endif

ifneq ($(ENABLE_JETKVM_AUDIO_CGO),0)
ifneq ($(TARGET_PLATFORM),jetkvm)
$(error ENABLE_JETKVM_AUDIO_CGO requires TARGET_PLATFORM=jetkvm)
endif
ifeq ($(CGO_ENABLED),0)
$(error ENABLE_JETKVM_AUDIO_CGO requires CGO_ENABLED=1)
endif
endif

GO_BUILD_TAGS := netgo,timetzdata,nomsgpack
ifeq ($(ENABLE_SYNC_TRACE),1)
	GO_BUILD_TAGS := $(GO_BUILD_TAGS),synctrace
endif
ifeq ($(ENABLE_JETKVM_NATIVE_CGO),1)
	GO_BUILD_TAGS := $(GO_BUILD_TAGS),jetkvm_native_cgo
endif
ifeq ($(ENABLE_JETKVM_AUDIO_CGO),1)
	GO_BUILD_TAGS := $(GO_BUILD_TAGS),jetkvm_audio_cgo
endif

GO_BUILD_ARGS := -tags $(GO_BUILD_TAGS)

GO_RELEASE_BUILD_ARGS := -trimpath $(GO_BUILD_ARGS)
GO_LDFLAGS := \
  -s -w \
  -X $(PROMETHEUS_TAG).Branch=$(BRANCH) \
  -X $(PROMETHEUS_TAG).BuildDate=$(BUILDDATE) \
  -X $(PROMETHEUS_TAG).Revision=$(REVISION) \
  -X $(KVM_PKG_NAME).builtTimestamp=$(BUILDTS)

GO_ARGS := GOOS=linux GOARCH=$(TARGET_ARCH) ARCHFLAGS="-arch $(TARGET_ARCH)" CGO_ENABLED=$(CGO_ENABLED)
ifeq ($(TARGET_ARCH),arm)
	GO_ARGS := $(GO_ARGS) GOARM=$(TARGET_GOARM)
endif

# Custom OpenSSL with devcrypto hardware acceleration support
SSL_LIBS_DIR ?= /opt/jetkvm-ssl-libs/install

ifeq ($(CGO_ENABLED),1)
ifeq ($(TARGET_ARCH),arm64)
	GO_ARGS := $(GO_ARGS) CC="aarch64-linux-gnu-gcc" LD="aarch64-linux-gnu-ld"
endif
ifeq ($(ENABLE_JETKVM_NATIVE_CGO),1)
ifneq ($(wildcard $(BUILDKIT_PATH)),)
	# Check if custom OpenSSL with devcrypto exists, use it for hardware crypto acceleration
	ifneq ($(wildcard $(SSL_LIBS_DIR)/lib64/libssl.a),)
		SSL_CFLAGS := -I$(SSL_LIBS_DIR)/include
		SSL_LDFLAGS := -L$(SSL_LIBS_DIR)/lib64 -L$(SSL_LIBS_DIR)/lib
	else ifneq ($(wildcard $(SSL_LIBS_DIR)/lib/libssl.a),)
		SSL_CFLAGS := -I$(SSL_LIBS_DIR)/include
		SSL_LDFLAGS := -L$(SSL_LIBS_DIR)/lib
	else
		# Fall back to buildkit's OpenSSL (no devcrypto support)
		SSL_CFLAGS :=
		SSL_LDFLAGS :=
	endif
	GO_ARGS := $(GO_ARGS) \
		CGO_CFLAGS="-I$(BUILDKIT_PATH)/$(BUILDKIT_FLAVOR)/include -I$(BUILDKIT_PATH)/$(BUILDKIT_FLAVOR)/sysroot/usr/include $(SSL_CFLAGS)" \
		CGO_LDFLAGS="$(SSL_LDFLAGS) -L$(BUILDKIT_PATH)/$(BUILDKIT_FLAVOR)/lib -L$(BUILDKIT_PATH)/$(BUILDKIT_FLAVOR)/sysroot/usr/lib -lrockit -lrockchip_mpp -lrga -Wl,-Bstatic -lssl -lcrypto -lz -Wl,-Bdynamic -lpthread -lm" \
		CC="$(BUILDKIT_PATH)/bin/$(BUILDKIT_FLAVOR)-gcc" \
		LD="$(BUILDKIT_PATH)/bin/$(BUILDKIT_FLAVOR)-ld"
endif
endif
endif

GO_CMD := $(GO_ARGS) go

BIN_DIR := $(shell pwd)/bin
BIN_SUFFIX :=
ifneq ($(TARGET_PLATFORM),jetkvm)
BIN_SUFFIX := -$(TARGET_PLATFORM)
endif
BIN_OUTPUT := $(BIN_DIR)/jetkvm_app$(BIN_SUFFIX)

TEST_DIRS := $(shell find . -name "*_test.go" -type f -exec dirname {} \; | sort -u)

# Build ALSA and Opus static libs for ARM in /opt/jetkvm-audio-libs
# Skip if already built (check for .built marker files)
# Requires x86_64 architecture (cross-compiler is x86_64)
AUDIO_LIBS_DIR := /opt/jetkvm-audio-libs
build_audio_deps:
	@if [ "$(ENABLE_JETKVM_AUDIO_CGO)" != "1" ]; then \
		echo "Skipping audio dependency build (ENABLE_JETKVM_AUDIO_CGO=0)"; \
	elif [ -f "$(AUDIO_LIBS_DIR)/alsa-lib-1.2.14/.built" ] && \
	    [ -f "$(AUDIO_LIBS_DIR)/opus-1.5.2/.built" ] && \
	    [ -f "$(AUDIO_LIBS_DIR)/speexdsp-1.2.1/.built" ]; then \
		echo "Audio dependencies already built, skipping..."; \
	elif [ "$$(uname -m)" != "x86_64" ]; then \
		echo "ERROR: Audio deps build requires x86_64 architecture."; \
		echo "Current arch: $$(uname -m)"; \
		echo "Use Docker build mode (remove --disable-docker flag) or run on x86_64 host."; \
		exit 1; \
	else \
		bash .devcontainer/install_audio_deps.sh; \
	fi

test:
	go test ./...

# E2E tests - builds, sets up mock server, runs all tests including OTA
test_e2e: build_dev
	@if [ -z "$(DEVICE_IP)" ]; then \
		read -p "Device IP: " device_ip; \
	else \
		device_ip="$(DEVICE_IP)"; \
	fi; \
	cd ui && npm ci && npx playwright install chromium && cd ..; \
	./scripts/test_local_update.sh "$$device_ip" "$(BIN_OUTPUT)" "$(VERSION_DEV)"

lint:
	go vet ./...

check: lint test

# Comprehensive lint with auto-fix (Go + UI)
lint-fix: build_audio_deps
	@echo "Running golangci-lint with auto-fix..."
	@mkdir -p static && touch static/.gitkeep
	golangci-lint run --fix --verbose
	@echo "Running UI lint with auto-fix..."
	@cd ui && npm ci && npm run lint:fix
	@echo "All linting completed!"

build_native:
	@if [ "$(ENABLE_JETKVM_NATIVE_CGO)" != "1" ]; then \
		echo "Skipping native build (ENABLE_JETKVM_NATIVE_CGO=0)"; \
	elif [ "$(SKIP_NATIVE_IF_EXISTS)" = "1" ] && [ -f "internal/hal/native/cgo/lib/libjknative.a" ]; then \
		echo "libjknative.a already exists, skipping native build..."; \
	else \
		echo "Building native..."; \
			CC="$(BUILDKIT_PATH)/bin/$(BUILDKIT_FLAVOR)-gcc" \
			LD="$(BUILDKIT_PATH)/bin/$(BUILDKIT_FLAVOR)-ld" \
			CMAKE_BUILD_TYPE=$(CMAKE_BUILD_TYPE) \
			./scripts/build_cgo.sh; \
	fi

build_dev:
	@if [ "$(CGO_ENABLED)" = "1" ] && [ "$(TARGET_ARCH)" = "arm" ] && [ ! -d "$(BUILDKIT_PATH)" ]; then \
		echo "Toolchain not found, running build_dev in Docker..."; \
		rm -rf internal/hal/native/cgo/build; \
		docker run --rm -v "$$(pwd):/build" \
			$(DOCKER_BUILD_TAG) make _build_dev_inner \
				TARGET_PLATFORM=$(TARGET_PLATFORM) \
				CGO_ENABLED=$(CGO_ENABLED) \
				ENABLE_JETKVM_NATIVE_CGO=$(ENABLE_JETKVM_NATIVE_CGO) \
				ENABLE_JETKVM_AUDIO_CGO=$(ENABLE_JETKVM_AUDIO_CGO) \
				VERSION_DEV=$(VERSION_DEV); \
	else \
		$(MAKE) _build_dev_inner \
			TARGET_PLATFORM=$(TARGET_PLATFORM) \
			CGO_ENABLED=$(CGO_ENABLED) \
			ENABLE_JETKVM_NATIVE_CGO=$(ENABLE_JETKVM_NATIVE_CGO) \
			ENABLE_JETKVM_AUDIO_CGO=$(ENABLE_JETKVM_AUDIO_CGO) \
			VERSION_DEV=$(VERSION_DEV); \
	fi

_build_dev_inner: build_native
	@echo "Building $(TARGET_PLATFORM) ($(TARGET_ARCH))... $(VERSION_DEV)"
	$(GO_CMD) build \
		-ldflags="$(GO_LDFLAGS) -X $(KVM_PKG_NAME).builtAppVersion=$(VERSION_DEV)" \
		$(GO_RELEASE_BUILD_ARGS) \
		-o $(BIN_OUTPUT) -v ./cmd

build_dev_jetkvm:
	$(MAKE) build_dev TARGET_PLATFORM=jetkvm CGO_ENABLED=0

build_dev_comet:
	$(MAKE) build_dev TARGET_PLATFORM=comet-pro CGO_ENABLED=0

build_dev_nanokvm:
	$(MAKE) build_dev TARGET_PLATFORM=nanokvm-pro CGO_ENABLED=0

build_dev_all: build_dev_jetkvm build_dev_comet build_dev_nanokvm

build_test2json:
	$(GO_CMD) build -o $(BIN_DIR)/test2json cmd/test2json

build_gotestsum:
	@echo "Building gotestsum..."
	$(GO_CMD) install gotest.tools/gotestsum@latest
	cp $(shell $(GO_CMD) env GOPATH)/bin/linux_$(TARGET_ARCH)/gotestsum $(BIN_DIR)/gotestsum

build_dev_test: build_test2json build_gotestsum
# collect all directories that contain tests
	@echo "Building tests for devices ..."
	@rm -rf $(BIN_DIR)/tests && mkdir -p $(BIN_DIR)/tests

	@cat resource/dev_test.sh > $(BIN_DIR)/tests/run_all_tests
	@for test in $(TEST_DIRS); do \
		test_pkg_name=$$(echo $$test | sed 's/^.\///g'); \
		test_pkg_full_name=$(KVM_PKG_NAME)/$$(echo $$test | sed 's/^.\///g'); \
		test_filename=$$(echo $$test_pkg_name | sed 's/\//__/g')_test; \
		$(GO_CMD) test -v \
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

git_check_dev:
	@if [ "$$(git rev-parse --abbrev-ref HEAD)" != "dev" ]; then \
		echo "Error: Must be on 'dev' branch"; exit 1; \
	fi
	@if [ -n "$$(git status --porcelain)" ]; then \
		echo "Error: Working tree is dirty. Commit or stash changes."; exit 1; \
	fi
	@git fetch origin dev
	@if [ "$$(git rev-parse HEAD)" != "$$(git rev-parse origin/dev)" ]; then \
		echo "Error: Local dev is not up-to-date with origin/dev"; exit 1; \
	fi
	@command -v gh >/dev/null 2>&1 || { echo "Error: gh CLI not installed"; exit 1; }
	@gh auth status >/dev/null 2>&1 || { echo "Error: gh CLI not authenticated. Run 'gh auth login'"; exit 1; }

dev_release: git_check_dev
	@echo "═══════════════════════════════════════════════════════"
	@echo "  DEV Release"
	@echo "═══════════════════════════════════════════════════════"
	@echo "  Version: $(VERSION_DEV)"
	@echo "  Tag:     release/$(VERSION_DEV)"
	@echo "  Branch:  $$(git rev-parse --abbrev-ref HEAD)"
	@echo "  Commit:  $$(git rev-parse --short HEAD)"
	@echo "  Time:    $$(date -u +%FT%T%z)"
	@echo "═══════════════════════════════════════════════════════"
	@read -p "Proceed? [y/N] " confirm && [ "$$confirm" = "y" ] || exit 1
	$(MAKE) check frontend build_dev VERSION_DEV=$(VERSION_DEV)
	@read -p "Test on device before release? [y/N] " test_confirm; \
	if [ "$$test_confirm" = "y" ]; then \
		read -p "Device IP: " device_ip; \
		echo "Installing Playwright dependencies..."; \
		cd ui && npm ci && npx playwright install --with-deps chromium && cd ..; \
		./scripts/test_local_update.sh "$$device_ip" bin/jetkvm_app $(VERSION_DEV) || exit 1; \
	fi
	@echo "Uploading device app to R2..."
	@shasum -a 256 bin/jetkvm_app | cut -d ' ' -f 1 > bin/jetkvm_app.sha256
	rclone copyto bin/jetkvm_app r2://jetkvm-update/app/$(VERSION_DEV)/jetkvm_app
	rclone copyto bin/jetkvm_app.sha256 r2://jetkvm-update/app/$(VERSION_DEV)/jetkvm_app.sha256
	./scripts/deploy_cloud_app.sh -v $(VERSION_DEV) --skip-confirmation
	@git tag release/$(VERSION_DEV)
	@git push origin release/$(VERSION_DEV)
	gh release create release/$(VERSION_DEV) bin/jetkvm_app bin/jetkvm_app.sha256 --prerelease --generate-notes
	@echo "✓ Released: release/$(VERSION_DEV)"

# NOTE: VERSION is passed explicitly for consistency with build_dev (see comment above).
# While VERSION is static, passing it explicitly ensures the pattern is consistent
# and prevents issues if VERSION ever becomes dynamic.
build_release:
	@if [ "$(CGO_ENABLED)" = "1" ] && [ "$(TARGET_ARCH)" = "arm" ] && [ ! -d "$(BUILDKIT_PATH)" ]; then \
		echo "Toolchain not found, running build_release in Docker..."; \
		rm -rf internal/hal/native/cgo/build; \
		docker run --rm -v "$$(pwd):/build" \
			$(DOCKER_BUILD_TAG) make _build_release_inner \
				TARGET_PLATFORM=$(TARGET_PLATFORM) \
				CGO_ENABLED=$(CGO_ENABLED) \
				ENABLE_JETKVM_NATIVE_CGO=$(ENABLE_JETKVM_NATIVE_CGO) \
				ENABLE_JETKVM_AUDIO_CGO=$(ENABLE_JETKVM_AUDIO_CGO) \
				VERSION=$(VERSION); \
	else \
		$(MAKE) _build_release_inner \
			TARGET_PLATFORM=$(TARGET_PLATFORM) \
			CGO_ENABLED=$(CGO_ENABLED) \
			ENABLE_JETKVM_NATIVE_CGO=$(ENABLE_JETKVM_NATIVE_CGO) \
			ENABLE_JETKVM_AUDIO_CGO=$(ENABLE_JETKVM_AUDIO_CGO) \
			VERSION=$(VERSION); \
	fi

_build_release_inner: build_native
	@echo "Building release $(TARGET_PLATFORM) ($(TARGET_ARCH))..."
	$(GO_CMD) build \
		-ldflags="$(GO_LDFLAGS) -X $(KVM_PKG_NAME).builtAppVersion=$(VERSION)" \
		$(GO_RELEASE_BUILD_ARGS) \
		-o $(BIN_OUTPUT) ./cmd

release: git_check_dev
	@if rclone lsf r2://jetkvm-update/app/$(VERSION)/ 2>/dev/null | grep -q "jetkvm_app"; then \
		echo "Error: Version $(VERSION) already exists in R2"; exit 1; \
	fi
	@latest_dev=$$(curl -s "https://api.jetkvm.com/releases?deviceId=123&prerelease=true" | jq -r '.appVersion // ""'); \
		if ! echo "$$latest_dev" | grep -q "^$(VERSION)-dev"; then \
			echo ""; \
			echo "⚠️  Warning: No dev release found for $(VERSION)"; \
			echo "   Latest pre-release: $$latest_dev"; \
			echo ""; \
			read -p "Release production without prior dev release? [y/N] " confirm && [ "$$confirm" = "y" ] || exit 1; \
		fi
	@echo "═══════════════════════════════════════════════════════"
	@echo "  PRODUCTION Release"
	@echo "═══════════════════════════════════════════════════════"
	@echo "  Version: $(VERSION)"
	@echo "  Tag:     release/$(VERSION)"
	@echo "  Branch:  $$(git rev-parse --abbrev-ref HEAD)"
	@echo "  Commit:  $$(git rev-parse --short HEAD)"
	@echo "  Time:    $$(date -u +%FT%T%z)"
	@echo "═══════════════════════════════════════════════════════"
	@read -p "Proceed with PRODUCTION release? [y/N] " confirm && [ "$$confirm" = "y" ] || exit 1
	$(MAKE) check frontend build_release VERSION=$(VERSION)
	@read -p "Test on device before release? [y/N] " test_confirm; \
	if [ "$$test_confirm" = "y" ]; then \
		read -p "Device IP: " device_ip; \
		echo "Installing Playwright dependencies..."; \
		cd ui && npm ci && npx playwright install --with-deps chromium && cd ..; \
		./scripts/test_local_update.sh "$$device_ip" bin/jetkvm_app $(VERSION) || exit 1; \
	fi
	@echo "Uploading device app to R2..."
	@shasum -a 256 bin/jetkvm_app | cut -d ' ' -f 1 > bin/jetkvm_app.sha256
	rclone copyto bin/jetkvm_app r2://jetkvm-update/app/$(VERSION)/jetkvm_app
	rclone copyto bin/jetkvm_app.sha256 r2://jetkvm-update/app/$(VERSION)/jetkvm_app.sha256
	./scripts/deploy_cloud_app.sh -v $(VERSION) --set-as-default --skip-confirmation
	@git tag release/$(VERSION)
	@git push origin release/$(VERSION)
	prev_prod=$$(gh release list --exclude-drafts --exclude-pre-releases --limit 1 --json tagName --jq '.[0].tagName'); \
	gh release create release/$(VERSION) bin/jetkvm_app bin/jetkvm_app.sha256 \
		--title "$(VERSION)" \
		--generate-notes \
		--notes-start-tag "$$prev_prod" \
		--draft
	@echo ""
	@echo "✓ Released: release/$(VERSION)"
	@echo ""
	@echo "Next: Run 'make bump-version' to prepare for next release cycle"

bump-version:
	@next_default=$$(echo $(VERSION) | awk -F. '{print $$1"."$$2"."$$3+1}'); \
		echo "Current version: $(VERSION)"; \
		read -p "Next version [$$next_default]: " next_ver; \
		next_ver=$${next_ver:-$$next_default}; \
		if ! echo "$$next_ver" | grep -qE '^[0-9]+\.[0-9]+\.[0-9]+$$'; then \
			echo "Error: Invalid version '$$next_ver'. Must be semver format (e.g., 1.2.3)"; \
			exit 1; \
		fi; \
		sed -i 's/^VERSION := .*/VERSION := '"$$next_ver"'/' Makefile && \
		git add Makefile && \
		git commit -m "Bump version to $$next_ver" && \
		git push && \
		echo "✓ Bumped to $$next_ver"
