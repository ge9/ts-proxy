TS_DIR=$(go list -m -f '{{.Dir}}' tailscale.com | grep -v "pkg/mod")
MY_DIR=$(cd $(dirname $0);pwd)
if [ -z "$TS_DIR" ]; then
  go mod vendor
  TS_DIR=vendor/tailscale.com
fi
echo $TS_DIR
cd $TS_DIR
grep -s "//go:build" ipn/localapi/cert.go | \
grep -v "android" || { \
    patch -p0 < $MY_DIR/cert.go.patch && \
    patch -p0 < $MY_DIR/disabled_stubs.go.patch; \
}
