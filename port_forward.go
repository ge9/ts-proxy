package tsproxy

import (
	"io"
	"log"
	"net"
	"sync"
	"time"
)

func (t *TsProxy) ForwardTCP(bind, connect string, useTLS bool) error {
	var ln net.Listener
	var err error
	bind = resolveTshost(t.tsServer, t.tsServer.Hostname, bind)
	connect = resolveTshost(t.tsServer, t.tsServer.Hostname, connect)
	if useTLS {
		ln, err = t.tsServer.ListenTLS("tcp", bind)
	} else {
		ln, err = listenTCP(t.tsServer, bind)
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
		if t.debug {
			log.Printf("[TCP] Accept %s at %s", src.RemoteAddr().String(), ln.Addr().String())
		}
		go func(src net.Conn) {
			defer src.Close()
			// ターゲットへ接続
			dst, err := dialAny(t.tsServer, "tcp", connect)
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
func (t *TsProxy) ForwardUDP(bind, connect string) error {
	bind = resolveTshost(t.tsServer, t.tsServer.Hostname, bind)
	connect = resolveTshost(t.tsServer, t.tsServer.Hostname, connect)
	pc, err := listenUDP(t.tsServer, bind)
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
			if t.debug {
				log.Printf("[UDP] Read: from %s on %s", clientAddr.String(), pc.LocalAddr().String())
			}
			clientKey := clientAddr.String()
			now := time.Now()

			mu.Lock()
			//cleanup
			if now.Sub(lastCleanup) > time.Duration(t.udpTimeout) {
				for k, s := range sessions {
					if now.Sub(s.lastActive) > time.Duration(t.udpTimeout) {
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
				dstConn, err := dialAny(t.tsServer, "udp", connect)
				if err != nil && t.debug {
					log.Printf("[UDP] Dial failed: %v", err)
				} else {
					if t.debug {
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
