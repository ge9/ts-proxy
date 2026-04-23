package tsproxy

import (
	"io"
	"log"
	"net"
	"sync"
	"time"
	"tailscale.com/tsnet"
)

// ForwardUDP using Tailscale's Listen/Accept API (correct way)
func (t *TsProxy) ForwardUDPFixed(bind, connect string) error {
	bind = resolveTshost(t.tsServer, t.tsServer.Hostname, bind)
	connect = resolveTshost(t.tsServer, t.tsServer.Hostname, connect)
	
	// Use Listen instead of ListenPacket!
	ln, err := listenUDPAsListener(t.tsServer, bind)
	if err != nil {
		log.Printf("[UDP] Listen failed: %v", err)
		return err
	}
	defer ln.Close()
	
	if t.debug {
		log.Printf("[UDP] Listening on %s, forwarding to %s", ln.Addr(), connect)
	}
	
	for {
		// Accept each UDP "connection" (flow from a specific client)
		conn, err := ln.Accept()
		if err != nil {
			log.Printf("[UDP] Accept error: %v", err)
			return err
		}
		
		if t.debug {
			log.Printf("[UDP] Accepted connection from %s", conn.RemoteAddr())
		}
		
		// Handle this UDP flow in a goroutine
		go t.handleUDPConn(conn, connect)
	}
}

func (t *TsProxy) handleUDPConn(clientConn net.Conn, connect string) {
	defer clientConn.Close()
	
	// Dial to destination
	dstConn, err := dialAny(t.tsServer, "udp", connect)
	if err != nil {
		if t.debug {
			log.Printf("[UDP] Dial failed (%s): %v", connect, err)
		}
		return
	}
	defer dstConn.Close()
	
	if t.debug {
		log.Printf("[UDP] Connected: %s <-> %s <-> %s", 
			clientConn.RemoteAddr(), clientConn.LocalAddr(), dstConn.RemoteAddr())
	}
	
	// Bidirectional copy with timeout
	var wg sync.WaitGroup
	wg.Add(2)
	
	// Client -> Destination
	go func() {
		defer wg.Done()
		buf := make([]byte, 65535)
		for {
			clientConn.SetReadDeadline(time.Now().Add(time.Duration(t.udpTimeout) * time.Second))
			n, err := clientConn.Read(buf)
			if err != nil {
				if t.debug && err != io.EOF {
					log.Printf("[UDP] Client read error: %v", err)
				}
				return
			}
			if t.debug {
				log.Printf("[UDP] Client->Dst: %d bytes", n)
			}
			_, err = dstConn.Write(buf[:n])
			if err != nil {
				if t.debug {
					log.Printf("[UDP] Dst write error: %v", err)
				}
				return
			}
		}
	}()
	
	// Destination -> Client
	go func() {
		defer wg.Done()
		buf := make([]byte, 65535)
		for {
			dstConn.SetReadDeadline(time.Now().Add(time.Duration(t.udpTimeout) * time.Second))
			n, err := dstConn.Read(buf)
			if err != nil {
				if t.debug && err != io.EOF {
					log.Printf("[UDP] Dst read error: %v", err)
				}
				return
			}
			if t.debug {
				log.Printf("[UDP] Dst->Client: %d bytes", n)
			}
			_, err = clientConn.Write(buf[:n])
			if err != nil {
				if t.debug {
					log.Printf("[UDP] Client write error: %v", err)
				}
				return
			}
		}
	}()
	
	wg.Wait()
	if t.debug {
		log.Printf("[UDP] Connection closed: %s", clientConn.RemoteAddr())
	}
}

// listenUDPAsListener returns a net.Listener for UDP (Tailscale's way)
func listenUDPAsListener(tsServer *tsnet.Server, addr string) (net.Listener, error) {
	if isTailscaleIPPortString(addr) {
		// Use Listen for Tailscale addresses (not ListenPacket!)
		return tsServer.Listen("udp", addr)
	}
	// For non-Tailscale addresses, still use standard net
	return net.Listen("udp", addr)
}
