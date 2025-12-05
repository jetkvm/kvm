BRANCH    := $(shell git rev-parse --abbrev-ref HEAD)
BUILDDATE := $(shell date -u +%FT%T%z)
BUILDTS   := $(shell date -u +%s)
REVISION  := $(shell git rev-parse HEAD)
VERSION := 0.5.0
VERSION_DEV := $(VERSION)-dev$(shell date -u +%Y%m%d%H%M)

PROMETHEUS_TAG := github.com/prometheus/common/version
KVM_PKG_NAME := github.com/jetkvm/kvm

BUILDKIT_FLAVOR := arm-rockchip830-linux-uclibcgnueabihf
BUILDKIT_PATH ?= /opt/jetkvm-native-buildkit
DOCKER_BUILD_TAG ?= ghcr.io/jetkvm/buildkit:latest
SKIP_NATIVE_IF_EXISTS ?= 0
SKIP_UI_BUILD ?= 0
ENABLE_SYNC_TRACE ?= 0

CMAKE_BUILD_TYPE ?= Release

GO_BUILD_ARGS := -tags netgo,timetzdata,nomsgpack
ifeq ($(ENABLE_SYNC_TRACE), 1)
	GO_BUILD_ARGS := $(GO_BUILD_ARGS),synctrace
endif

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

test:
	go test ./...

lint:
	go vet ./...

check: lint test

build_native:
	@if [ "$(SKIP_NATIVE_IF_EXISTS)" = "1" ] && [ -f "internal/native/cgo/lib/libjknative.a" ]; then \
		echo "libjknative.a already exists, skipping native build..."; \
	else \
		echo "Building native..."; \
			CC="$(BUILDKIT_PATH)/bin/$(BUILDKIT_FLAVOR)-gcc" \
			LD="$(BUILDKIT_PATH)/bin/$(BUILDKIT_FLAVOR)-ld" \
			CMAKE_BUILD_TYPE=$(CMAKE_BUILD_TYPE) \
			./scripts/build_cgo.sh; \
	fi

# NOTE: VERSION_DEV must be explicitly passed to nested make invocations.
# VERSION_DEV contains $(shell date ...) which gets re-evaluated when a new make
# process starts. Without passing it explicitly, a minute boundary crossed during
# the build would cause version mismatch between what's displayed and what's built.
build_dev:
	@if [ ! -d "$(BUILDKIT_PATH)" ]; then \
		echo "Toolchain not found, running build_dev in Docker..."; \
		rm -rf internal/native/cgo/build; \
		docker run --rm -v "$$(pwd):/build" \
			$(DOCKER_BUILD_TAG) make _build_dev_inner VERSION_DEV=$(VERSION_DEV); \
	else \
		$(MAKE) _build_dev_inner VERSION_DEV=$(VERSION_DEV); \
	fi

_build_dev_inner: build_native
	@echo "Building..."
	$(GO_CMD) build \
		-ldflags="$(GO_LDFLAGS) -X $(KVM_PKG_NAME).builtAppVersion=$(VERSION_DEV)" \
		$(GO_RELEASE_BUILD_ARGS) \
		-o $(BIN_DIR)/jetkvm_app -v cmd/main.go

build_test2json:
	$(GO_CMD) build -o $(BIN_DIR)/test2json cmd/test2json

build_gotestsum:
	@echo "Building gotestsum..."
	$(GO_CMD) install gotest.tools/gotestsum@latest
	cp $(shell $(GO_CMD) env GOPATH)/bin/linux_arm/gotestsum $(BIN_DIR)/gotestsum

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
	$(MAKE) check frontend build_dev
	@read -p "Test on device before release? [y/N] " test_confirm; \
	if [ "$$test_confirm" = "y" ]; then \
		read -p "Device IP: " device_ip; \
		./scripts/test_release_on_device.sh "$$device_ip" bin/jetkvm_app test $(VERSION_DEV) || exit 1; \
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
	@if [ ! -d "$(BUILDKIT_PATH)" ]; then \
		echo "Toolchain not found, running build_release in Docker..."; \
		rm -rf internal/native/cgo/build; \
		docker run --rm -v "$$(pwd):/build" \
			$(DOCKER_BUILD_TAG) make _build_release_inner VERSION=$(VERSION); \
	else \
		$(MAKE) _build_release_inner VERSION=$(VERSION); \
	fi

_build_release_inner: build_native
	@echo "Building release..."
	$(GO_CMD) build \
		-ldflags="$(GO_LDFLAGS) -X $(KVM_PKG_NAME).builtAppVersion=$(VERSION)" \
		$(GO_RELEASE_BUILD_ARGS) \
		-o bin/jetkvm_app cmd/main.go

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
	$(MAKE) check frontend build_release
	@read -p "Test on device before release? [y/N] " test_confirm; \
	if [ "$$test_confirm" = "y" ]; then \
		read -p "Device IP: " device_ip; \
		./scripts/test_release_on_device.sh "$$device_ip" bin/jetkvm_app test $(VERSION) || exit 1; \
	fi
	@echo "Uploading device app to R2..."
	@shasum -a 256 bin/jetkvm_app | cut -d ' ' -f 1 > bin/jetkvm_app.sha256
	rclone copyto bin/jetkvm_app r2://jetkvm-update/app/$(VERSION)/jetkvm_app
	rclone copyto bin/jetkvm_app.sha256 r2://jetkvm-update/app/$(VERSION)/jetkvm_app.sha256
	./scripts/deploy_cloud_app.sh -v $(VERSION) --set-as-default --skip-confirmation
	@git tag release/$(VERSION)
	@git push origin release/$(VERSION)
	gh release create release/$(VERSION) bin/jetkvm_app bin/jetkvm_app.sha256 --generate-notes
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
