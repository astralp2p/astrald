# Every build a deployer performs, named once.
#
# VERSION derives from git describe, so an image tag names the exact commit it
# was built from; override IMAGE to push to a registry namespace.

IMAGE   ?= astrald
VERSION ?= $(shell git describe --tags --always --dirty)

.PHONY: build image

# Static binaries into bin/. Pure-Go SQLite makes CGO_ENABLED=0 work; the ./
# prefix matters — `go build .` at the repo root builds an empty stub. The build
# flags are the Dockerfile build stage's: -trimpath keeps the build directory out
# of the binary, and -s -w drop the symbol table and DWARF. One binary, one
# recipe.
build:
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/astrald ./cmd/astrald
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/astral-query ./cmd/astral-query

# The image, tagged with the version it was built from and as latest.
image:
	docker build -t $(IMAGE):$(VERSION) -t $(IMAGE):latest .
