BRANCH    ?= $(shell git rev-parse --abbrev-ref HEAD)
BUILDDATE ?= $(shell date -u +%FT%T%z)
BUILDTS   ?= $(shell date -u +%s)
REVISION  ?= $(shell git rev-parse HEAD)
VERSION_DEV := 0.4.0-dev$(shell date +%Y%m%d%H%M)
VERSION := 0.3.9

PROMETHEUS_TAG := github.com/prometheus/common/version
KVM_PKG_NAME := github.com/jetkvm/kvm

GO_LDFLAGS := \
  -s -w \
  -X $(PROMETHEUS_TAG).Branch=$(BRANCH) \
  -X $(PROMETHEUS_TAG).BuildDate=$(BUILDDATE) \
  -X $(PROMETHEUS_TAG).Revision=$(REVISION) \
  -X $(KVM_PKG_NAME).builtTimestamp=$(BUILDTS)

hash_resource:
	@shasum -a 256 resource/jetkvm_native | cut -d ' ' -f 1 > resource/jetkvm_native.sha256

build_dev: hash_resource
	@echo "Building..."
	GOOS=linux GOARCH=arm GOARM=7 go build -ldflags="$(GO_LDFLAGS) -X $(KVM_PKG_NAME).builtAppVersion=$(VERSION_DEV)" -o bin/jetkvm_app cmd/main.go

build_test2json:
	GOOS=linux GOARCH=arm GOARM=7 go build -o bin/test2json cmd/test2json

build_dev_test: build_test2json
# collect all directories that contain tests
	@echo "Building tests for devices ..."
	@rm -rf bin/tests && mkdir -p bin/tests
	GOOS=linux GOARCH=arm GOARM=7 \
		go test -v \
		-ldflags="$(GO_LDFLAGS) -X $(KVM_PKG_NAME).builtAppVersion=$(VERSION_DEV)" \
		-c -o bin/tests ./...; \

	@cat resource/dev_test.sh > bin/tests/run_all_tests
	@for test in bin/tests/*.test; do \
		chmod +x $$test; \
		base_name=$$(basename $$test); \
		echo "runTest ./$$base_name" >> bin/tests/run_all_tests; \
	done; \
	chmod +x bin/tests/run_all_tests; \
	cp bin/test2json bin/tests/; \
	chmod +x bin/tests/test2json; \
	tar czfv device-tests.tar.gz -C bin/tests .

frontend:
	cd ui && npm ci && npm run build:device

dev_release: frontend build_dev
	@echo "Uploading release..."
	@shasum -a 256 bin/jetkvm_app | cut -d ' ' -f 1 > bin/jetkvm_app.sha256
	rclone copyto bin/jetkvm_app r2://jetkvm-update/app/$(VERSION_DEV)/jetkvm_app
	rclone copyto bin/jetkvm_app.sha256 r2://jetkvm-update/app/$(VERSION_DEV)/jetkvm_app.sha256

build_release: frontend hash_resource
	@echo "Building release..."
	GOOS=linux GOARCH=arm GOARM=7 go build -ldflags="$(GO_LDFLAGS) -X $(KVM_PKG_NAME).builtAppVersion=$(VERSION)" -o bin/jetkvm_app cmd/main.go

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
