package tsproxy

import (
	"context"
	"errors"
	"net"
	"time"

	"github.com/ge9/socks5"
)

// host is required and must be tailscale IP (":1080" or "0.0.0.0:1080" is not allowed)
func ServeSOCKS(bind, tcp4, tcp6, udp4, udp6 string) {
	bind = resolveTSAddr(bind)
	h, _, _ := net.SplitHostPort(bind)
	server, _ := socks5.NewClassicServer(bind, h, "", "", tcpTimeout, udpTimeout)
	server.ListenTCP = func(network string, laddr string) (net.Listener, error) {
		return tsServer.Listen(network, laddr)
	}
	server.ListenUDP = func(network, address string) (net.PacketConn, error) {
		pk, e := tsServer.ListenPacket(network, address)
		return pk, e
	}
	server.BindOutUDP = func(network string, laddr string) (net.PacketConn, error) {
		p, e := socks5.BindOutUDP(network, laddr)
		println("pp", network, laddr, p.LocalAddr().String())
		return p, e
	}

	//NOTE: in the socks5 lib, the second argument is always ""
	server.DialTCP = func(network string, _, raddr string) (net.Conn, error) {
		ra, err := net.ResolveTCPAddr("tcp", raddr) // or socks5.Resolve(network, raddr)
		if err != nil {
			return nil, err
		}
		var a2 *net.TCPAddr
		if ra.IP.To4() != nil { //IPv4
			a2, _ = net.ResolveTCPAddr("tcp", tcp4)
		} else { //IPv6
			a2, _ = net.ResolveTCPAddr("tcp", tcp6)
		}
		return net.DialTCP(network, a2, ra)
	}
	if udp4 == "disabled" || udp6 == "disabled" { //single stack
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
			return Newdelayed46UDPConn(udp4, udp6), nil
		}
	}

	server.ListenAndServe(nil)
}

func ForwardSOCKS(bind, connect string) {
	bind = resolveTSAddr(bind)
	connect = resolveTSAddr(connect)
	h, p, _ := net.SplitHostPort(bind)
	if h == "" {
		h = "0.0.0.0" //This seems to work in IPv6. Empty string won't work due to socks5's UDP() implementation
	}
	server, _ := socks5.NewClassicServer(h+":"+p, h, "", "", tcpTimeout, udpTimeout) //socks5 lib accepts IP in both of the first two arguments...?
	client, _ := socks5.NewClient(connect, "", "", tcpTimeout, udpTimeout)
	client.DialTCP = func(network string, laddr, raddr string) (net.Conn, error) {
		println(laddr, raddr)
		a, err := net.ResolveTCPAddr(network, raddr)
		if err != nil {
			return nil, err
		}
		return tsServer.Dial(context.Background(), network, a.String())
	}
	server.DialTCP = func(network string, laddr, raddr string) (net.Conn, error) {
		a, err := net.ResolveTCPAddr(network, raddr)
		if err != nil {
			return nil, err
		}
		return client.Dial(network, a.String())
	}
	server.BindOutUDP = func(network string, laa string) (net.PacketConn, error) {
		if err := client.Negotiate(nil); err != nil {
			return nil, err
		}
		a, h, p := socks5.ATYPIPv4, []byte{0x00, 0x00, 0x00, 0x00}, []byte{0x00, 0x00} //these address and port are never used. works even for IPv6.
		rp, err := client.Request(socks5.NewRequest(socks5.CmdUDP, a, h, p))
		if err != nil {
			return nil, err
		}
		c, err := tsServer.Dial(context.Background(), "udp", rp.Address())
		uc := proxyUDPConn{UDPConn: c}
		return uc, err
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
