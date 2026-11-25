package server

import (
	"errors"
	"log"
	"time"

	"SOCKS5_lab/server/socks5"
	"golang.org/x/sys/unix"
)

const (
	bufSize          = 32 * 1024
	handshakeTimeout = 30 * time.Second
	connectTimeout   = 15 * time.Second
	closeTimeout     = 5 * time.Second
)

type connState uint8

const (
	stGreeting connState = iota
	stRequest
	stResolving
	stConnecting
	stRelay
	stClosing
)

type Conn struct {
	srv   *Server
	state connState

	cfd int
	rfd int
	cEv uint32
	rEv uint32

	c2r *ring
	r2c *ring

	cEOF  bool
	rEOF  bool
	cShut bool
	rShut bool

	flushThenClose bool
	closed         bool

	port  uint16
	dnsQ  *lookup
	timer *Timer
}

func (s *Server) newConn(fd int) {
	_ = unix.SetsockoptInt(fd, unix.IPPROTO_TCP, unix.TCP_NODELAY, 1)
	c := &Conn{
		srv: s,
		cfd: fd,
		rfd: -1,
		c2r: newRing(bufSize),
		r2c: newRing(bufSize),
	}
	c.setTimer(handshakeTimeout, c.close)
	c.update()
}

func (c *Conn) HandleEvent(fd int, ev uint32) {
	if c.closed {
		return
	}
	switch fd {
	case c.cfd:
		c.onClient(ev)
	case c.rfd:
		c.onRemote(ev)
	}
	c.update()
}

func (c *Conn) onClient(ev uint32) {
	if ev&evErr != 0 {
		c.close()
		return
	}
	if ev&(evIn|evHup) != 0 {
		c.readClient()
		if c.closed {
			return
		}
	}
	if ev&(evOut|evHup) != 0 {
		c.flushClient()
	}
}

func (c *Conn) wantClientRead() bool {
	return !c.cEOF && !c.flushThenClose && c.c2r.free() > 0
}

func (c *Conn) readClient() {
	if !c.wantClientRead() {
		return
	}
	eof, err := readInto(c.cfd, c.c2r)
	if err != nil {
		c.close()
		return
	}
	if eof {
		c.onClientEOF()
		return
	}
	switch c.state {
	case stGreeting:
		c.tryGreeting()
	case stRequest:
		c.tryRequest()
	case stRelay:
		c.flushRemote()
	}
}

func (c *Conn) onClientEOF() {
	switch c.state {
	case stGreeting, stRequest, stClosing:
		c.close()
	case stResolving, stConnecting:
		c.cEOF = true
	case stRelay:
		c.cEOF = true
		c.flushRemote()
	}
}

func (c *Conn) flushClient() {
	if c.cShut {
		return
	}
	if err := writeFrom(c.cfd, c.r2c); err != nil {
		c.close()
		return
	}
	if c.r2c.size > 0 {
		return
	}
	if c.flushThenClose {
		c.close()
		return
	}
	if c.state == stRelay && c.rEOF {
		_ = unix.Shutdown(c.cfd, unix.SHUT_WR)
		c.cShut = true
		c.checkDone()
	}
}

func (c *Conn) tryGreeting() {
	var tmp [socks5.MaxGreetingLen]byte
	n := c.c2r.peek(tmp[:])
	used, noAuth, err := socks5.ParseGreeting(tmp[:n])
	if err != nil {
		c.close()
		return
	}
	if used == 0 {
		return
	}
	c.c2r.consume(used)
	if !noAuth {
		c.queueFinal([]byte{socks5.Version5, socks5.MethodNoAcceptable})
		return
	}
	c.r2c.put([]byte{socks5.Version5, socks5.MethodNoAuth})
	c.state = stRequest
	c.flushClient()
	if !c.closed {
		c.tryRequest()
	}
}

func (c *Conn) tryRequest() {
	var tmp [socks5.MaxRequestLen]byte
	n := c.c2r.peek(tmp[:])
	req, used, err := socks5.ParseRequest(tmp[:n])
	switch {
	case errors.Is(err, socks5.ErrCmdNotSupported):
		c.fail(socks5.RepCmdNotSupported)
		return
	case errors.Is(err, socks5.ErrAtypNotSupported):
		c.fail(socks5.RepAtypNotSupported)
		return
	case err != nil:
		c.close()
		return
	case used == 0:
		return
	}
	c.c2r.consume(used)
	c.port = req.Port
	if req.Atyp == socks5.AtypIPv4 {
		c.startConnect(req.IP)
		return
	}
	c.startResolve(req.Domain)
}

func (c *Conn) fail(rep byte) {
	if c.closed {
		return
	}
	c.cancelLookup()
	c.closeRemote()
	c.queueFinal(socks5.BuildReply(rep, [4]byte{}, 0))
}

func (c *Conn) queueFinal(msg []byte) {
	c.r2c.put(msg)
	c.state = stClosing
	c.flushThenClose = true
	c.setTimer(closeTimeout, c.close)
	c.flushClient()
}

func (c *Conn) startResolve(name string) {
	c.state = stResolving
	c.cancelTimer()
	q, err := c.srv.dns.Resolve(name, c.onResolved)
	if err != nil {
		log.Printf("resolve %q: %v", name, err)
		c.fail(socks5.RepHostUnreachable)
		return
	}
	c.dnsQ = q
}

func (c *Conn) onResolved(ip [4]byte, err error) {
	c.dnsQ = nil
	if c.closed {
		return
	}
	if err != nil {
		log.Printf("resolve: %v", err)
		c.fail(socks5.RepHostUnreachable)
	} else {
		c.startConnect(ip)
	}
	c.update()
}

func (c *Conn) cancelLookup() {
	if c.dnsQ != nil {
		c.srv.dns.Cancel(c.dnsQ)
		c.dnsQ = nil
	}
}

func (c *Conn) startConnect(ip [4]byte) {
	fd, err := unix.Socket(unix.AF_INET, unix.SOCK_STREAM|unix.SOCK_NONBLOCK|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		c.fail(socks5.RepGeneralFailure)
		return
	}
	_ = unix.SetsockoptInt(fd, unix.IPPROTO_TCP, unix.TCP_NODELAY, 1)
	err = unix.Connect(fd, &unix.SockaddrInet4{Port: int(c.port), Addr: ip})
	if err != nil && err != unix.EINPROGRESS {
		unix.Close(fd)
		c.fail(repFromErr(err))
		return
	}
	c.rfd = fd
	c.state = stConnecting
	c.setTimer(connectTimeout, func() {
		c.fail(socks5.RepHostUnreachable)
		c.update()
	})
}

func (c *Conn) finishConnect() {
	soErr, err := unix.GetsockoptInt(c.rfd, unix.SOL_SOCKET, unix.SO_ERROR)
	if err != nil {
		c.fail(socks5.RepGeneralFailure)
		return
	}
	if soErr != 0 {
		c.fail(repFromErr(unix.Errno(soErr)))
		return
	}
	if _, err := unix.Getpeername(c.rfd); err != nil {
		if err == unix.ENOTCONN {
			return
		}
		c.fail(socks5.RepGeneralFailure)
		return
	}

	var ip [4]byte
	var port uint16
	if sa, err := unix.Getsockname(c.rfd); err == nil {
		if s4, ok := sa.(*unix.SockaddrInet4); ok {
			ip = s4.Addr
			port = uint16(s4.Port)
		}
	}
	c.cancelTimer()
	c.r2c.put(socks5.BuildReply(socks5.RepSucceeded, ip, port))
	c.state = stRelay
	c.flushClient()
	if c.closed {
		return
	}
	c.flushRemote()
}

func repFromErr(err error) byte {
	switch err {
	case unix.ECONNREFUSED:
		return socks5.RepConnRefused
	case unix.ENETUNREACH:
		return socks5.RepNetUnreachable
	case unix.EHOSTUNREACH, unix.ETIMEDOUT:
		return socks5.RepHostUnreachable
	case unix.EACCES, unix.EPERM:
		return socks5.RepNotAllowed
	}
	return socks5.RepGeneralFailure
}

func (c *Conn) onRemote(ev uint32) {
	switch c.state {
	case stConnecting:
		c.finishConnect()
	case stRelay:
		if ev&evErr != 0 {
			c.close()
			return
		}
		if ev&(evIn|evHup) != 0 {
			c.readRemote()
			if c.closed {
				return
			}
		}
		if ev&(evOut|evHup) != 0 {
			c.flushRemote()
		}
	}
}

func (c *Conn) readRemote() {
	if c.rEOF || c.r2c.free() == 0 {
		return
	}
	eof, err := readInto(c.rfd, c.r2c)
	if err != nil {
		c.close()
		return
	}
	if eof {
		c.rEOF = true
	}
	c.flushClient()
}

func (c *Conn) flushRemote() {
	if c.state != stRelay || c.rShut {
		return
	}
	if err := writeFrom(c.rfd, c.c2r); err != nil {
		c.close()
		return
	}
	if c.cEOF && c.c2r.size == 0 {
		_ = unix.Shutdown(c.rfd, unix.SHUT_WR)
		c.rShut = true
		c.checkDone()
	}
}

func (c *Conn) checkDone() {
	if c.cShut && c.rShut {
		c.close()
	}
}

func (c *Conn) update() {
	if c.closed {
		return
	}
	var want uint32
	if c.wantClientRead() {
		want |= evIn
	}
	if c.r2c.size > 0 && !c.cShut {
		want |= evOut
	}
	if !c.setEvents(c.cfd, &c.cEv, want) {
		return
	}
	if c.rfd < 0 {
		return
	}
	want = 0
	switch c.state {
	case stConnecting:
		want = evOut
	case stRelay:
		if !c.rEOF && c.r2c.free() > 0 {
			want |= evIn
		}
		if !c.rShut && c.c2r.size > 0 {
			want |= evOut
		}
	}
	c.setEvents(c.rfd, &c.rEv, want)
}

func (c *Conn) setEvents(fd int, cur *uint32, want uint32) bool {
	if want == *cur {
		return true
	}
	var err error
	switch {
	case *cur == 0:
		err = c.srv.sel.Add(fd, want, c)
	case want == 0:
		c.srv.sel.Remove(fd)
	default:
		err = c.srv.sel.Modify(fd, want)
	}
	if err != nil {
		log.Printf("epoll: %v", err)
		c.close()
		return false
	}
	*cur = want
	return true
}

func (c *Conn) setTimer(d time.Duration, fn func()) {
	c.cancelTimer()
	c.timer = c.srv.sel.After(d, fn)
}

func (c *Conn) cancelTimer() {
	if c.timer != nil {
		c.srv.sel.Cancel(c.timer)
		c.timer = nil
	}
}

func (c *Conn) closeRemote() {
	if c.rfd < 0 {
		return
	}
	if c.rEv != 0 {
		c.srv.sel.Remove(c.rfd)
	}
	unix.Close(c.rfd)
	c.rfd = -1
	c.rEv = 0
}

func (c *Conn) close() {
	if c.closed {
		return
	}
	c.closed = true
	c.cancelTimer()
	c.cancelLookup()
	c.closeRemote()
	if c.cfd >= 0 {
		if c.cEv != 0 {
			c.srv.sel.Remove(c.cfd)
		}
		unix.Close(c.cfd)
		c.cfd = -1
		c.cEv = 0
	}
}

func readInto(fd int, r *ring) (eof bool, err error) {
	seg := r.writeSeg()
	if len(seg) == 0 {
		return false, nil
	}
	n, err := unix.Read(fd, seg)
	if err != nil {
		if err == unix.EAGAIN || err == unix.EINTR {
			return false, nil
		}
		return false, err
	}
	if n == 0 {
		return true, nil
	}
	r.produce(n)
	return false, nil
}

func writeFrom(fd int, r *ring) error {
	for r.size > 0 {
		seg := r.readSeg()
		n, err := unix.Write(fd, seg)
		if err != nil {
			if err == unix.EAGAIN {
				return nil
			}
			if err == unix.EINTR {
				continue
			}
			return err
		}
		r.consume(n)
		if n < len(seg) {
			return nil
		}
	}
	return nil
}
