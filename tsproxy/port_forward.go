package tsproxy

import (
	"context"
	"io"
	"log"
	"net"
	"sync"
	"time"
)

var (
	_, tsIPv4Block, _ = net.ParseCIDR("100.64.0.0/10")       // Tailscale IPv4 (CGNAT)
	_, tsIPv6Block, _ = net.ParseCIDR("fd7a:115c:a1e0::/64") // Tailscale IPv6
)

func isTailscaleAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}

	return tsIPv4Block.Contains(ip) || tsIPv6Block.Contains(ip)
}

func dialAny(network, addr string) (net.Conn, error) {
	if isTailscaleAddr(addr) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return tsServer.Dial(ctx, network, addr)
	}
	return net.Dial(network, addr)
}

func listenTCP(addr string) (net.Listener, error) {
	if isTailscaleAddr(addr) {
		return tsServer.Listen("tcp", addr)
	}
	return net.Listen("tcp", addr)
}

func listenUDP(addr string) (net.PacketConn, error) {
	if isTailscaleAddr(addr) {
		return tsServer.ListenPacket("udp", addr)
	}
	return net.ListenPacket("udp", addr)
}

func ForwardTCP(bind, connect string, useTLS bool) error {
	var ln net.Listener
	var err error
	bind = resolveTSAddr(bind)
	connect = resolveTSAddr(connect)
	if useTLS {
		ln, err = tsServer.ListenTLS("tcp", bind)
	} else {
		ln, err = listenTCP(bind)
	}
	if err != nil {
		log.Printf("[TCP] Listen Failed: %v", err)
		return err
	}

	defer ln.Close()
	for {
		src, err := ln.Accept()
		if err != nil {
			log.Printf("[TCP] Accept Error:%v", err)
			return err
		}
		if debug {
			log.Printf("[TCP] Accept %s at %s", src.RemoteAddr().String(), ln.Addr().String())
		}
		go func(src net.Conn) {
			defer src.Close()
			// ターゲットへ接続
			dst, err := dialAny("tcp", connect)
			if err != nil {
				log.Printf("[TCP] Dial failed (%s): %v", connect, err)
				return
			}
			defer dst.Close()

			go io.Copy(dst, src)
			io.Copy(src, dst)
		}(src)
	}
}

type udpSession struct {
	conn       net.Conn
	lastActive time.Time
}

// Basically AI-generated
func ForwardUDP(bind, connect string) error {
	bind = resolveTSAddr(bind)
	connect = resolveTSAddr(connect)
	pc, err := listenUDP(bind)
	if err != nil {
		return err
	}
	go func() {
		defer pc.Close()

		sessions := make(map[string]*udpSession)
		var mu sync.Mutex

		lastCleanup := time.Now()

		buf := make([]byte, 4096)

		for {
			// 1. パケット受信
			n, clientAddr, err := pc.ReadFrom(buf)
			if err != nil {
				log.Printf("[UDP] Read error: %v", err)
				return
			}
			if debug {
				log.Printf("[UDP] Read: from %s on %s", clientAddr.String(), pc.LocalAddr().String())
			}
			clientKey := clientAddr.String()
			now := time.Now()

			mu.Lock()
			//cleanup
			if now.Sub(lastCleanup) > time.Duration(udpTimeout) {
				for k, s := range sessions {
					if now.Sub(s.lastActive) > time.Duration(udpTimeout) {
						s.conn.Close()
						delete(sessions, k)
					}
				}
				lastCleanup = now
			}

			session, exists := sessions[clientKey]
			if exists {
				session.lastActive = now
				session.conn.Write(buf[:n])
			} else {
				dstConn, err := dialAny("udp", connect)
				if err != nil && debug {
					log.Printf("[UDP] Dial failed: %v", err)
				} else {
					if debug {
						log.Printf("[UDP] Dial: %s to %s", dstConn.LocalAddr().String(), dstConn.RemoteAddr().String())
					}
					session = &udpSession{conn: dstConn, lastActive: now}
					sessions[clientKey] = session
					dstConn.Write(buf[:n])

					// process reply packets
					go func(c net.Conn, target net.Addr, k string) {
						defer c.Close()
						b := make([]byte, 4096)
						for {
							m, err := c.Read(b)
							if err != nil {
								return // closed
							}
							mu.Lock()
							if s, ok := sessions[k]; ok {
								s.lastActive = time.Now()
								pc.WriteTo(b[:m], target)
							}
							mu.Unlock()
						}
					}(dstConn, clientAddr, clientKey)
				}
			}
			mu.Unlock()
		}
	}()
	return nil
}
