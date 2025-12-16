package tsproxy

import (
	"context"
	"fmt"
	"log"
	"net"
	"strings"

	"github.com/ge9/socks5"
	"github.com/wlynxg/anet"
	"tailscale.com/net/netmon"
	"tailscale.com/tsnet"
)

var tcpTimeout, udpTimeout = 1100, 330
var tsServer *tsnet.Server
var debug = false

func Setup(tsServer0 *tsnet.Server, tcpTimeout0, udpTimeout0 int, debug0 bool) {
	tcpTimeout = tcpTimeout0
	udpTimeout = udpTimeout0
	tsServer = tsServer0
	debug = debug0
	socks5.Debug = debug0
	anetPatch()
	if _, err := tsServer.Up(context.Background()); err != nil {
		log.Fatalf("Failed to start tsnet: %v", err)
	}
}
func anetPatch() {
	// A minimal patch for Android
	// https://github.com/wlynxg/anet
	// https://github.com/Asutorufa/tailscale/commit/d7bdd6d72d4297313ffc447e6d51ef5429c92db7#diff-07877f4150707b91b9910fc07d035a3da02088dbf1cda414f24142e567b27ef4
	netmon.RegisterInterfaceGetter(func() ([]netmon.Interface, error) {
		ifs, err := anet.Interfaces()
		if err != nil {
			return nil, fmt.Errorf("anet.Interfaces: %w", err)
		}
		ret := make([]netmon.Interface, len(ifs))
		for i := range ifs {
			addrs, err := anet.InterfaceAddrsByInterface(&ifs[i])
			if err != nil {
				return nil, fmt.Errorf("ifs[%d].Addrs: %w", i, err)
			}
			ret[i] = netmon.Interface{
				Interface: &ifs[i],
				AltAddrs:  addrs,
			}
		}
		return ret, nil
	})
}

// if addr is Tailscale address, resolve it. An IPv4 address is returned.
func resolveTSAddr(addr string) string {
	host, port, _ := net.SplitHostPort(addr)
	if strings.HasSuffix(host, ".tshost") {
		c, _ := tsServer.Dial(context.Background(), "udp", host[:len(host)-7]+":53") //we can use any port
		tshost, _, _ := net.SplitHostPort(c.RemoteAddr().String())
		return net.JoinHostPort(tshost, port)
	}
	return addr
}
