SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -euc

GO := /usr/local/go/bin/go
PROJECT_DIR := $(shell dirname $(realpath $(firstword $(MAKEFILE_LIST))))
MODULE_NAME := $(shell "$(GO)" list -m)

CONTAINER_TOOL ?= docker

##@ General

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

.PHONY: fmt
fmt: ## Run go fmt against code.
	$(GO) fmt ./...

.PHONY: vet
vet: ## Run go vet against code.
	$(GO) vet ./...

.PHONY: check-licenses
check-licenses: ## Check licenses of dependencies.
	@$(GO) run github.com/google/go-licenses@latest report ./...

.PHONY: test
test: fmt vet
	ginkgo run -race -cover -vet="" $$($(GO) list ./... | grep -v /e2e | sed "s~$$($(GO) list -m)/~~")

.PHONY: test-e2e
test-e2e: fmt vet ## Run the e2e tests. This usually requires root privileges.
	$(GO) test ./e2e/ -v -ginkgo.v
	$(MAKE) cleanup-test-e2e

.PHONY: cleanup-test-e2e
cleanup-test-e2e: ## Tear down the netns used for e2e tests.
	@ip netns del "manager-testing" 2>&1 | grep -v "No such file or directory" || true

##@ Build and Release

VERSION = 0.0.1-dev
CONTAINER_REGISTRY = ghcr.io/soliddowant
PUSH_ALL ?= false

BUILD_DIR := $(PROJECT_DIR)/build
BINARY_DIR = $(BUILD_DIR)/binaries
BINARY_PLATFORMS = linux/amd64 linux/arm64
BINARY_NAME = gateway-route-manager

GO_SOURCE_FILES := $(shell find . \( -name '*.go' ! -name '*_test.go' \))
GO_LDFLAGS := -s -w

LOCALOS := $(shell uname -s | tr '[:upper:]' '[:lower:]')
LOCALARCH := $(shell uname -m | sed 's/x86_64/amd64/')
LOCAL_BINARY_PATH := $(BINARY_DIR)/$(LOCALOS)/$(LOCALARCH)/$(BINARY_NAME)

$(BINARY_DIR)/%/$(BINARY_NAME): $(GO_SOURCE_FILES)
	@mkdir -p "$(@D)"
	@CGO_ENABLED=0 GOOS="$(word 1,$(subst /, ,$*))" GOARCH="$(word 2,$(subst /, ,$*))" $(GO) build -ldflags="$(GO_LDFLAGS)" -o "$@" ./cmd/

LOCAL_BUILDERS += binary
.PHONY: binary
binary: $(LOCAL_BINARY_PATH)	## Build the binary for the local platform.

ALL_BUILDERS += binary-all
.PHONY: binary-all
binary-all: $(BINARY_PLATFORMS:%=$(BINARY_DIR)/%/$(BINARY_NAME))	## Build the binary for all supported platforms.

LICENSE_DIR = $(BUILD_DIR)/licenses
GO_DEPENDENCIES_LICENSE_DIR = $(LICENSE_DIR)/go-dependencies
BUILT_LICENSES := $(LICENSE_DIR)/LICENSE $(GO_DEPENDENCIES_LICENSE_DIR)

$(BUILT_LICENSES) &: go.mod LICENSE
	@mkdir -p "$(LICENSE_DIR)"
	@cp LICENSE "$(LICENSE_DIR)"
	@rm -rf "$(GO_DEPENDENCIES_LICENSE_DIR)"
	@$(GO) run github.com/google/go-licenses@latest save ./... --save_path="$(GO_DEPENDENCIES_LICENSE_DIR)" --ignore "$(MODULE_NAME)"

ALL_BUILDERS += licenses
.PHONY: licenses
licenses: $(BUILT_LICENSES)	## Gather licenses of the project and its dependencies.

TARBALL_DIR = $(BUILD_DIR)/tarballs
LOCAL_TARBALL_PATH := $(TARBALL_DIR)/$(LOCALOS)/$(LOCALARCH)/$(BINARY_NAME).tar.gz

$(TARBALL_DIR)/%/$(BINARY_NAME).tar.gz: $(BINARY_DIR)/%/$(BINARY_NAME) licenses
	@mkdir -p "$(@D)"
	@tar -czf "$@" -C "$(BINARY_DIR)/$*" "$(BINARY_NAME)" -C "$(dir $(LICENSE_DIR))" "$(notdir $(LICENSE_DIR))"

PHONY += tarball
LOCAL_BUILDERS += tarball
tarball: $(LOCAL_TARBALL_PATH)	## Create a tarball with the binary and licenses for the local platform.

PHONY += tarball-all
ALL_BUILDERS += tarball-all
tarball-all: $(BINARY_PLATFORMS:%=$(TARBALL_DIR)/%/$(BINARY_NAME).tar.gz)	## Create tarballs with the binary and licenses for all supported platforms.

INCLUDE_LATEST = $(PUSH_ALL)

CONTAINER_IMAGE_TAGS = $(CONTAINER_REGISTRY)/$(BINARY_NAME):$(VERSION) $(if $(filter true,$(INCLUDE_LATEST)),$(CONTAINER_REGISTRY)/$(BINARY_NAME):latest,)

CONTAINER_IMAGE_extended_TAGS = $(CONTAINER_IMAGE_TAGS:%=%-extended)
CONTAINER_IMAGE_extended_ARGS = BASE_IMAGE=extended

CONTAINER_BUILD_LABEL_VARS = org.opencontainers.image.source=https://github.com/solidDoWant/gateway-route-manager org.opencontainers.image.licenses=AGPL-3.0
CONTAINER_BUILD_LABELS := $(foreach var,$(CONTAINER_BUILD_LABEL_VARS),--label $(var))
CONTAINER_PLATFORMS := $(BINARY_PLATFORMS)

.PHONY: container-image-%
container-image-%: TAG_TO_USE = $(if $(filter standard,$*),$(CONTAINER_IMAGE_TAGS),$(CONTAINER_IMAGE_$*_TAGS))
container-image-%: ARGS_TO_USE = $(if $(filter standard,$*),$(CONTAINER_IMAGE_ARGS),$(CONTAINER_IMAGE_$*_ARGS))
container-image-%: binary licenses	## Build the container image for the local platform.
	$(CONTAINER_TOOL) buildx build --platform linux/$(LOCALARCH) $(TAG_TO_USE:%=-t %) --load $(CONTAINER_BUILD_LABELS) $(ARGS_TO_USE:%=--build-arg %) .

CONTAINER_IMAGE_BUILDERS += container-image-standard
CONTAINER_IMAGE_BUILDERS += container-image-extended
LOCAL_BUILDERS += $(CONTAINER_IMAGE_BUILDERS)

.PHONY: container-image
container-image: container-image-standard	## Build the container image for the local platform.

.PHONY: container-images
container-images: $(CONTAINER_IMAGE_BUILDERS)	## Build all container images for the local platform.

CONTAINER_MANIFEST_PUSH ?= $(PUSH_ALL)

.PHONY: container-manifest-%
container-manifest-%: PUSH_ARG = $(if $(findstring t,$(CONTAINER_MANIFEST_PUSH)),--push)
container-manifest-%: TAG_TO_USE = $(if $(filter standard,$*),$(CONTAINER_IMAGE_TAGS),$(CONTAINER_IMAGE_$*_TAGS))
container-manifest-%: ARGS_TO_USE = $(if $(filter standard,$*),$(CONTAINER_IMAGE_ARGS),$(CONTAINER_IMAGE_$*_ARGS))
container-manifest-%: $(CONTAINER_PLATFORMS:%=$(BINARY_DIR)/%/$(BINARY_NAME)) licenses	## Build and optionally push the container image for all supported platforms.
	@docker buildx build $(CONTAINER_PLATFORMS:%=--platform %) $(PUSH_ARG) $(TAG_TO_USE:%=-t %) $(CONTAINER_BUILD_LABELS) $(ARGS_TO_USE:%=--build-arg %) .

CONTAINER_MANIFEST_BUILDERS += container-manifest-standard
CONTAINER_MANIFEST_BUILDERS += container-manifest-extended
ALL_BUILDERS += $(CONTAINER_MANIFEST_BUILDERS)

.PHONY: container-manifest
container-manifest: container-manifest-standard	## Build and optionally push the standard container image for all supported platforms.

.PHONY: container-manifests
container-manifests: $(CONTAINER_MANIFEST_BUILDERS)	## Build and optionally push all container images for all supported platforms.

.PHONY: build
build: manifests generate fmt vet schemas $(LOCAL_BUILDERS) ## Builds all local outputs (binaries, tarballs, licenses, etc.).

.PHONY: build-all
build-all: $(ALL_BUILDERS)	## Builds all outputs for all supported platforms (binaries, tarballs, licenses, etc.).

RELEASE_DIR = $(BUILD_DIR)/releases/$(VERSION)

PHONY += release
release: TAG = v$(VERSION)
release: CP_CMDS = $(foreach PLATFORM,$(BINARY_PLATFORMS),cp $(TARBALL_DIR)/$(PLATFORM)/$(BINARY_NAME).tar.gz $(RELEASE_DIR)/$(BINARY_NAME)-$(VERSION)-$(subst /,-,$(PLATFORM)).tar.gz &&)
release: CP_CMDS += true
release: SAFETY_PREFIX = $(if $(findstring t,$(PUSH_ALL)),,echo)
release: build-all	## Create a GitHub release including all tarballs for all supported platforms. Requires the GitHub CLI (gh).
	@mkdir -p $(RELEASE_DIR)
	@gh auth status
	@$(CP_CMDS)
	@$(SAFETY_PREFIX) git tag -a $(TAG) -m "Release $(TAG)"
	@$(SAFETY_PREFIX) git push origin
	@$(SAFETY_PREFIX) git push origin --tags
	@$(SAFETY_PREFIX) gh release create $(TAG) --generate-notes --latest --verify-tag "$(RELEASE_DIR)"/*

.PHONY: clean
clean:	INCLUDE_LATEST = true
clean:	## Clean up all build artifacts.
	@rm -rf $(BUILD_DIR) $(WORKING_DIR) $(HELM_CHART_DIR)/charts coverprofile.out
	@docker image rm -f $(CONTAINER_IMAGE_TAGS) $(CONTAINER_IMAGE_extended_TAGS) 2> /dev/null > /dev/null || true
