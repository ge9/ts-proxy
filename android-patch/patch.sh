MY_DIR=$(cd $(dirname $0);pwd)
cd $(dirname $MY_DIR)
TS_DIR=$(go list -m -f '{{.Dir}}' tailscale.com | grep -v "pkg/mod")
if [ -z "$TS_DIR" ]; then
  go mod vendor
  TS_DIR=vendor/tailscale.com
fi
echo Tailscale source files are located at: $TS_DIR
cd $TS_DIR
grep -s "//go:build" ipn/localapi/cert.go | \
grep -v "android" || { \
    patch -p0 < $MY_DIR/cert.go.patch && \
    patch -p0 < $MY_DIR/disabled_stubs.go.patch; \
}
