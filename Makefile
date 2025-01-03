BRANCH    ?= $(shell git rev-parse --abbrev-ref HEAD)
BUILDDATE ?= $(shell date --iso-8601=seconds)
REVISION  ?= $(shell git rev-parse HEAD)
VERSION_DEV := 0.3.5-dev$(shell date +%Y%m%d%H%M)
VERSION := 0.3.4

GO_LDFLAGS := \
  -s -w \
  -X github.com/prometheus/common/version.Branch=$(BRANCH) \
  -X github.com/prometheus/common/version.BuildDate=$(BUILDDATE) \
  -X github.com/prometheus/common/version.Revision=$(REVISION)

hash_resource:
	@shasum -a 256 resource/jetkvm_native | cut -d ' ' -f 1 > resource/jetkvm_native.sha256

build_dev: hash_resource
	@echo "Building..."
	GOOS=linux GOARCH=arm GOARM=7 go build -ldflags="$(GO_LDFLAGS) -X kvm.builtAppVersion=$(VERSION_DEV)" -o bin/jetkvm_app cmd/main.go

frontend:
	cd ui && npm ci && npm run build:device

dev_release: build_dev
	@echo "Uploading release..."
	@shasum -a 256 bin/jetkvm_app | cut -d ' ' -f 1 > bin/jetkvm_app.sha256
	rclone copyto bin/jetkvm_app r2://jetkvm-update/app/$(VERSION_DEV)/jetkvm_app
	rclone copyto bin/jetkvm_app.sha256 r2://jetkvm-update/app/$(VERSION_DEV)/jetkvm_app.sha256

build_release: frontend hash_resource
	@echo "Building release..."
	GOOS=linux GOARCH=arm GOARM=7 go build -ldflags="$(GO_LDFLAGS) -X kvm.builtAppVersion=$(VERSION)" -o bin/jetkvm_app cmd/main.go

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
