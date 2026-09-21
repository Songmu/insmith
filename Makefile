CURRENT_REVISION = $(shell git rev-parse --short HEAD)
BUILD_FLAGS = -trimpath -buildvcs=false
BUILD_LDFLAGS = -s -w -X github.com/Songmu/insmith.revision=$(CURRENT_REVISION)
u := $(if $(update),-u)

.PHONY: deps
deps:
	go get ${u}
	go mod tidy

.PHONY: devel-deps
devel-deps:
	go install github.com/Songmu/gocredits/cmd/gocredits@v0.5.0

.PHONY: test
test:
	go test

.PHONY: build
build:
	go build $(BUILD_FLAGS) -ldflags="$(BUILD_LDFLAGS)" ./cmd/insmith

.PHONY: install
install:
	go install $(BUILD_FLAGS) -ldflags="$(BUILD_LDFLAGS)" ./cmd/insmith

.PHONY: prepare-release
prepare-release: devel-deps
	go get
	go mod tidy
	gocredits -w
	git add go.mod CREDITS
	test ! -f go.sum || git add go.sum
