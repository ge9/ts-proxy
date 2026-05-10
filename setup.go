package tsproxy

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/netip"
	"os"
	"strings"
	"time"

	"github.com/ge9/socks5"
	"github.com/wlynxg/anet"
	"tailscale.com/net/netmon"
	"tailscale.com/net/tsaddr"
	"tailscale.com/tsnet"
)

// var tcpTimeout, udpTimeout = 1100, 330
// var tsServer *tsnet.Server
// var debug = false
type TsProxy struct {
	tsServer   *tsnet.Server
	tcpTimeout int
	udpTimeout int
	debug      bool
}

func NewTsProxy(tsServer0 *tsnet.Server, tcpTimeout0, udpTimeout0 int, debug0 bool) *TsProxy {
	t := &TsProxy{
		tcpTimeout: tcpTimeout0,
		udpTimeout: udpTimeout0,
		tsServer:   tsServer0,
		debug:      debug0,
	}
	//NOTE: Unfortunately, socks5.Debug can only be set globally
	socks5.Debug = debug0
	anetPatch()
	if _, err := t.tsServer.Up(context.Background()); err != nil {
		log.Fatalf("Failed to start tsnet: %v", err)
	}
	return t
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

// if addr is "*.tshost", resolve it. An IPv4 address is returned.
func resolveTshost(tsServer *tsnet.Server, hostname string, addr string) string {
	host, port, _ := net.SplitHostPort(addr)
	if strings.HasSuffix(host, ".tshost") {
		host = host[:len(host)-7]
		if host == "" {
			host = hostname
		}
		return net.JoinHostPort(resolveTailscaleIPv4(tsServer, host).String(), port)
	}
	return addr
}

func resolveTailscaleIPv4(s *tsnet.Server, hostname string) netip.Addr {
	lc, _ := s.LocalClient()
	status, _ := lc.Status(context.Background())
	for _, peer := range status.Peer {
		if strings.EqualFold(peer.HostName, hostname) {
			return firstIPv4(peer.TailscaleIPs)
		}
	}
	if strings.EqualFold(status.Self.HostName, hostname) {
		return firstIPv4(status.Self.TailscaleIPs)
	}
	panic("couldn't resolve tailscale host: " + hostname)
}

func firstIPv4(ips []netip.Addr) netip.Addr {
	for _, ip := range ips {
		if ip.Is4() {
			return ip
		}
	}
	return netip.Addr{}
}

func isTailscaleHost(s *tsnet.Server, hostname string) bool {
	lc, _ := s.LocalClient()
	status, _ := lc.Status(context.Background())
	for _, peer := range status.Peer {
		if strings.EqualFold(peer.HostName, hostname) || strings.HasPrefix(peer.DNSName, hostname+".") {
			return true
		}
	}
	if strings.EqualFold(status.Self.HostName, hostname) || strings.HasPrefix(status.Self.DNSName, hostname+".") {
		return true
	}
	return false
}

func isTailscaleIPPortString(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	return isTailscaleIPString(host)
}

func isTailscaleIPString(host string) bool {
	ip, e := netip.ParseAddr(host)
	if e != nil {
		return false
	}
	return tsaddr.IsTailscaleIP(ip)
}

func isTailscaleIPv4String(host string) bool {
	ip, e := netip.ParseAddr(host)
	if e != nil {
		return false
	}
	return tsaddr.IsTailscaleIPv4(ip)
}

func isTailscaleIPv6String(host string) bool {
	ip, e := netip.ParseAddr(host)
	if e != nil {
		return false
	}
	return tsaddr.TailscaleULARange().Contains(ip)
}
func tsDial(tsServer *tsnet.Server, network, addr string) (net.Conn, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return tsServer.Dial(ctx, network, addr)
}

func dialAny(tsServer *tsnet.Server, network, addr string) (net.Conn, error) {
	if isUnixAddr(addr) {
		return net.Dial("unix", addr)
	}
	if isTailscaleIPPortString(addr) {
		return tsDial(tsServer, network, addr)
	}
	return net.Dial(network, addr)
}

func isUnixAddr(addr string) bool {
	_, _, err := net.SplitHostPort(addr)
	return err != nil
}

func listenTCP(tsServer *tsnet.Server, addr string) (net.Listener, error) {
	if isUnixAddr(addr) {
		os.Remove(addr)
		return net.Listen("unix", addr)
	}
	if isTailscaleIPPortString(addr) {
		return tsServer.Listen("tcp", addr)
	}
	return net.Listen("tcp", addr)
}

func listenUDP(tsServer *tsnet.Server, addr string) (net.PacketConn, error) {
	if isTailscaleIPPortString(addr) {
		return tsServer.ListenPacket("udp", addr)
	}
	return net.ListenPacket("udp", addr)
}
