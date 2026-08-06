BINARY := asl3-gui
PKG := ./cmd/asl3-gui

.PHONY: build run test clean amd64 arm64 fmt

# CGO_ENABLED=0 everywhere: nothing in this project needs cgo, and
# disabling it produces a fully static binary with no dependency on a
# working C toolchain on the build machine.

build:
	CGO_ENABLED=0 go build -o bin/$(BINARY) $(PKG)

run: build
	./bin/$(BINARY)

test:
	go test ./...

fmt:
	gofmt -l -w .

# ASL3 (Debian 12/13) officially supports 64-bit amd64 and arm64 only --
# no 32-bit ARM, unlike HamVoIP's Pi Zero/1/2 support (confirmed against
# allstarlink.github.io/install/debian/install/).
amd64:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o bin/$(BINARY)-amd64 $(PKG)

arm64:
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o bin/$(BINARY)-arm64 $(PKG)

clean:
	rm -rf bin
