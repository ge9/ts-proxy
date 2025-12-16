GOOS ?= $(shell go env GOOS)

ts-proxy: *.go
ifeq ($(GOOS),android)
	$(MAKE) check-patch
	go build -ldflags "-checklinkname=0 -s -w" -trimpath .
else
	go build -ldflags "-s -w" -trimpath .
endif

.PHONY: check-patch
check-patch:
	grep "//go:build" vendor/tailscale.com/ipn/localapi/cert.go | \
	grep -v "android" || { \
	    go mod vendor; \
		patch -p0 < cert.go.patch && \
		patch -p0 < disabled_stubs.go.patch; \
	}
