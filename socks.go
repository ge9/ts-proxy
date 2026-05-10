package tsproxy

import (
	"errors"
	"log"
	"net"
	"time"

	"github.com/ge9/socks5"
)

func (t *TsProxy) baseSOCKSConfig(bind string) *socks5.Server {
	bind = resolveTshost(t.tsServer, t.tsServer.Hostname, bind)
	h, _, _ := net.SplitHostPort(bind)
	if h == "" {
		h = "0.0.0.0" //This seems to work in IPv6. Empty string won't work due to socks5's UDP() implementation
	}
	server, _ := socks5.NewClassicServer(bind, h, "", "", t.tcpTimeout, t.udpTimeout) //socks5 lib accepts IP in both of the first two arguments...?
	server.ListenTCP = func(_ string, laddr string) (net.Listener, error) {
		return listenTCP(t.tsServer, laddr)
	}
	server.ListenUDP = func(network, laddr string) (net.PacketConn, error) {
		return listenUDP(t.tsServer, laddr)
	}
	return server
}

func (t *TsProxy) ServeSOCKS(bind, tcp4, tcp6, udp4, udp6 string) {
	server := t.baseSOCKSConfig(bind)
	//NOTE: in the socks5 lib, the second argument is always ""
	server.DialTCP = func(network string, _, raddr string) (net.Conn, error) {
		ra, err := net.ResolveTCPAddr("tcp", raddr) // or socks5.Resolve(network, raddr)
		if err != nil {
			return nil, err
		}
		a2, _ := net.ResolveTCPAddr("tcp", tcp6)
		if ra.IP.To4() != nil { //IPv4
			a2, _ = net.ResolveTCPAddr("tcp", tcp4)
		}
		return net.DialTCP(network, a2, ra)
	}
	//default implementation for no outaddr_config
	server.BindOutUDP = func(network string, laddr string) (net.PacketConn, error) {
		return socks5.BindOutUDP(network, laddr)
	}
	if udp4 == "disabled" || udp6 == "disabled" { //v4 or v6 only
		udpOut := udp4
		if udp4 == "disabled" {
			udpOut = udp6
		}
		server.BindOutUDP = func(network string, laddr string) (net.PacketConn, error) {
			var la *net.UDPAddr
			if laddr != "" {
				var err error
				la, err = net.ResolveUDPAddr(network, laddr)
				if err != nil {
					return nil, err
				}
			} else {
				la, _ = net.ResolveUDPAddr(network, udpOut)
			}
			return net.ListenUDP(network, la)
		}
	} else if udp4 != "" || udp6 != "" {
		server.BindOutUDP = func(network string, laddr string) (net.PacketConn, error) {
			co := func(dstAddr net.Addr) (net.PacketConn, error) {
				network, address := "udp6", udp6
				if dstAddr.(*net.UDPAddr).IP.To4() != nil {
					network, address = "udp4", udp4
				}
				if t.debug {
					log.Printf("[delayedUDPConn initialized]: %s, BindAddr: %s, Dest: %s\n", network, address, dstAddr)
				}
				return net.ListenPacket(network, address)
			}
			return &delayedUDPConn{connOpener: co}, nil
		}
	}

	server.ListenAndServe(nil)
}

func (t *TsProxy) ForwardSOCKS(bind, connect string) {
	server := t.baseSOCKSConfig(bind)
	connect = resolveTshost(t.tsServer, t.tsServer.Hostname, connect)
	client, _ := socks5.NewClient(connect, "", "", t.tcpTimeout, t.udpTimeout)
	client.DialTCP = func(network string, laddr, raddr string) (net.Conn, error) {
		a, err := net.ResolveTCPAddr(network, raddr)
		if err != nil {
			return nil, err
		}
		return tsDial(t.tsServer, network, a.String())
	}
	server.DialTCP = func(network string, _, raddr string) (net.Conn, error) {
		a, err := net.ResolveTCPAddr(network, raddr)
		if err != nil {
			return nil, err
		}
		return client.Dial(network, a.String())
	}
	server.BindOutUDP = func(network string, _ string) (net.PacketConn, error) {
		if err := client.Negotiate(nil); err != nil {
			return nil, err
		}
		a, h, p := socks5.ATYPIPv4, []byte{0x00, 0x00, 0x00, 0x00}, []byte{0x00, 0x00} //these address and port are never used. works even for IPv6.
		rp, err := client.Request(socks5.NewRequest(socks5.CmdUDP, a, h, p))
		if err != nil {
			return nil, err
		}
		c, err := tsDial(t.tsServer, "udp", rp.Address())
		uc := proxyUDPConn{UDPConn: c}
		return uc, err
	}
	server.ListenAndServe(nil)
}

func (t *TsProxy) TailnetSOCKS(bind string) {
	server := t.baseSOCKSConfig(bind)
	server.DialTCP = func(network string, _, raddr string) (net.Conn, error) {
		return tsDial(t.tsServer, network, raddr)
	}
	server.BindOutUDP = func(network string, laddr string) (net.PacketConn, error) {
		return t.tsServer.ListenPacket(network, laddr)
	}
	server.ListenAndServe(nil)
}

// Combination of ServeSOCKS and TailnetSOCKS
func (t *TsProxy) DualSOCKS(bind, tcp4, tcp6, udp4, udp6 string) {
	server := t.baseSOCKSConfig(bind)
	server.DialTCP = func(network string, _, raddr string) (net.Conn, error) {
		host, _, _ := net.SplitHostPort(raddr)
		if isTailscaleHost(t.tsServer, host) || isTailscaleIPString(host) {
			return tsDial(t.tsServer, network, raddr)
		}
		ra, err := net.ResolveTCPAddr("tcp", raddr) // or socks5.Resolve(network, raddr)
		if err != nil {
			return nil, err
		}
		a2, _ := net.ResolveTCPAddr("tcp", tcp6)
		if ra.IP.To4() != nil { //IPv4
			a2, _ = net.ResolveTCPAddr("tcp", tcp4)
		}
		return net.DialTCP(network, a2, ra)
	}
	server.BindOutUDP = func(network string, laddr string) (net.PacketConn, error) {
		//TODO: support Tailscale domain address?
		ts4, ts6 := t.tsServer.TailscaleIPs()
		co := func(dstAddr net.Addr) (net.PacketConn, error) {
			if isTailscaleIPv4String(dstAddr.Network()) {
				return t.tsServer.ListenPacket("udp", ts4.String())
			}
			if isTailscaleIPv6String(dstAddr.Network()) {
				return t.tsServer.ListenPacket("udp", ts6.String())
			}
			network, address := "udp6", udp6
			if dstAddr.(*net.UDPAddr).IP.To4() != nil {
				network, address = "udp4", udp4
			}
			if t.debug {
				log.Printf("[BindOutUDP]: %s, BindAddr: %s, Dest: %s\n", network, address, dstAddr)
			}
			return net.ListenPacket(network, address)
		}
		return &delayedUDPConn{connOpener: co}, nil
	}

	server.ListenAndServe(nil)
}

// PacketConn implementation for SOCKS5 relay.
// We can change server implementation to allow domain address in communication (ReadFrom, WriteTo) with proxyUDPConn, but it's not implemented yet.
type proxyUDPConn struct {
	UDPConn net.Conn
}

// based on Read() in socks5 lib
func (p proxyUDPConn) ReadFrom(b []byte) (int, net.Addr, error) {
	n, err := p.UDPConn.Read(b)
	if err != nil {
		return 0, nil, err
	}
	d, err := socks5.NewDatagramFromBytes(b[0:n])
	if err != nil {
		return 0, nil, err
	}
	//assume no ATYPDomain here (though it may work)
	addr, _ := net.ResolveUDPAddr("udp", d.Address())
	n = copy(b, d.Data)
	return n, addr, nil
}

// based on Write() in socks5 lib
func (uc proxyUDPConn) WriteTo(b []byte, addr net.Addr) (int, error) {
	a, h, p, err := socks5.ParseAddress(addr.String())
	if err != nil {
		return 0, err
	}
	d := socks5.NewDatagram(a, h, p, b)
	b1 := d.Bytes()
	n, err := uc.UDPConn.Write(b1)
	if err != nil {
		return 0, err
	}
	if len(b1) != n {
		return 0, errors.New("not write full")
	}
	return len(b), nil
}
func (uc proxyUDPConn) Close() error                       { return uc.UDPConn.Close() }
func (uc proxyUDPConn) LocalAddr() net.Addr                { return uc.UDPConn.LocalAddr() } //is this ok...? (マップのキーとしてしか使わないので同値関係が一致していればいい気もする)
func (uc proxyUDPConn) SetDeadline(t time.Time) error      { return uc.UDPConn.SetDeadline(t) }
func (uc proxyUDPConn) SetReadDeadline(t time.Time) error  { return uc.UDPConn.SetReadDeadline(t) }
func (uc proxyUDPConn) SetWriteDeadline(t time.Time) error { return uc.UDPConn.SetWriteDeadline(t) }
