package tsproxy

import (
	"io"
	"log"
	"sync"
)

// ForwardUDPV3 - simplified, always use Read/Write
func (t *TsProxy) ForwardUDPV3(bind, connect string) error {
	bind = resolveTshost(t.tsServer, t.tsServer.Hostname, bind)
	connect = resolveTshost(t.tsServer, t.tsServer.Hostname, connect)
	
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
		conn, err := ln.Accept()
		if err != nil {
			log.Printf("[UDP] Accept error: %v", err)
			return err
		}
		
		if t.debug {
			log.Printf("[UDP] Accepted connection from %s", conn.RemoteAddr())
		}
		
		// Always use Read/Write, not ReadFrom/WriteTo
		go t.handleUDPConnSimple(conn, connect)
	}
}

func (t *TsProxy) handleUDPConnSimple(clientConn io.ReadWriteCloser, connect string) {
	defer clientConn.Close()
	
	dstConn, err := dialAny(t.tsServer, "udp", connect)
	if err != nil {
		if t.debug {
			log.Printf("[UDP] Dial failed (%s): %v", connect, err)
		}
		return
	}
	defer dstConn.Close()
	
	if t.debug {
		log.Printf("[UDP] Connected: client <-> %s", connect)
	}
	
	var wg sync.WaitGroup
	wg.Add(2)
	
	// Client -> Destination
	go func() {
		defer wg.Done()
		buf := make([]byte, 65535)
		for {
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
	
	// Destination -> Client (use Write, not WriteTo!)
	go func() {
		defer wg.Done()
		buf := make([]byte, 65535)
		for {
			n, err := dstConn.Read(buf)
			if err != nil {
				if t.debug && err != io.EOF {
					log.Printf("[UDP] Dst read error: %v", err)
				}
				return
			}
			if t.debug {
				log.Printf("[UDP] Dst->Client: %d bytes (using Write)", n)
			}
			// Use Write instead of WriteTo!
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
		log.Printf("[UDP] Connection closed")
	}
}
