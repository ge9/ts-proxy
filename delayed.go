package tsproxy

import (
	"errors"
	"net"
	"sync"
	"time"
)

// In UDP Associate, there are no means to decide if the UDP relay port is used for IPv4 or IPv6 outgoing traffic before the first UDP packet is sent.
// This is problematic when we want to specify outgoing address and still want to use both IPv4 and IPv6. The "delayed46UDPConn" will help in this situation.
// It's basically an UDP connection and implements net.PacketConn, but it "delays" the determination of IP family after the first WriteTo().
// Any ReadFrom() before the first WriteTo() will fail.

var ErrNotInitialized = errors.New("connection not initialized yet")

type delayedUDPConn struct {
	mu   sync.RWMutex
	conn net.PacketConn
	// bind address
	connOpener func(dstAddr net.Addr) (net.PacketConn, error)
	// deadlines are also delayed
	readDeadline  time.Time
	writeDeadline time.Time
}

func (d *delayedUDPConn) initConn(dstAddr net.Addr) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.conn != nil {
		return nil // already initialized (for some race condition maybe)
	}

	c, err := d.connOpener(dstAddr)
	if err != nil {
		return err
	}

	// apply deadlines
	if !d.readDeadline.IsZero() {
		c.SetReadDeadline(d.readDeadline)
	}
	if !d.writeDeadline.IsZero() {
		c.SetWriteDeadline(d.writeDeadline)
	}

	d.conn = c
	return nil
}

func (d *delayedUDPConn) WriteTo(p []byte, addr net.Addr) (n int, err error) {
	d.mu.RLock()
	if d.conn != nil {
		c := d.conn
		d.mu.RUnlock()
		return c.WriteTo(p, addr)
	}
	d.mu.RUnlock()

	if err := d.initConn(addr); err != nil {
		return 0, err
	}

	d.mu.RLock()
	defer d.mu.RUnlock()
	if d.conn == nil {
		return 0, errors.New("failed to initialize connection")
	}
	return d.conn.WriteTo(p, addr)
}

func (d *delayedUDPConn) ReadFrom(p []byte) (n int, addr net.Addr, err error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	// error before first WriteTo()
	if d.conn == nil {
		return 0, nil, ErrNotInitialized
	}
	return d.conn.ReadFrom(p)
}

func (d *delayedUDPConn) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.conn == nil {
		return nil
	}
	return d.conn.Close()
}

func (d *delayedUDPConn) LocalAddr() net.Addr {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if d.conn == nil {
		// Return zero address and port.
		// This will totally break the purpose of "UDPSrc" in the socks5 library,
		// but basically it doesn't affect full cone NAT behavior if udpTimeout is enough long.
		return &net.UDPAddr{}
	}
	return d.conn.LocalAddr()
}

func (d *delayedUDPConn) SetDeadline(t time.Time) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.readDeadline = t
	d.writeDeadline = t
	if d.conn != nil {
		return d.conn.SetDeadline(t)
	}
	return nil
}

func (d *delayedUDPConn) SetReadDeadline(t time.Time) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.readDeadline = t
	if d.conn != nil {
		return d.conn.SetReadDeadline(t)
	}
	return nil
}

func (d *delayedUDPConn) SetWriteDeadline(t time.Time) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.writeDeadline = t
	if d.conn != nil {
		return d.conn.SetWriteDeadline(t)
	}
	return nil
}
