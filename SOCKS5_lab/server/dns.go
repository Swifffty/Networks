package server

import (
	"bytes"
	"encoding/binary"
	"errors"
	"log"
	"math/rand/v2"
	"net/netip"
	"os"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

const (
	dnsTimeout     = 2 * time.Second
	dnsMaxAttempts = 3
	dnsMaxPending  = 60000
)

var (
	errDNSTimeout = errors.New("dns: timeout")
	errDNSFailed  = errors.New("dns: resolution failed")
	errDNSBadName = errors.New("dns: bad name")
	errDNSIgnore  = errors.New("dns: packet ignored")
)

func SystemResolver() (netip.AddrPort, error) {
	data, err := os.ReadFile("/etc/resolv.conf")
	if err != nil {
		return netip.AddrPort{}, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) >= 2 && f[0] == "nameserver" {
			a, err := netip.ParseAddr(f[1])
			if err == nil && a.Unmap().Is4() {
				return netip.AddrPortFrom(a.Unmap(), 53), nil
			}
		}
	}
	return netip.AddrPort{}, errors.New("no IPv4 nameserver found in /etc/resolv.conf")
}

func buildQuery(id uint16, name string) ([]byte, error) {
	name = strings.TrimSuffix(name, ".")
	if name == "" || len(name) > 253 {
		return nil, errDNSBadName
	}
	q := make([]byte, 12, 12+len(name)+6)
	binary.BigEndian.PutUint16(q[0:], id)
	binary.BigEndian.PutUint16(q[2:], 0x0100)
	binary.BigEndian.PutUint16(q[4:], 1)
	for _, label := range strings.Split(name, ".") {
		if len(label) == 0 || len(label) > 63 {
			return nil, errDNSBadName
		}
		q = append(q, byte(len(label)))
		q = append(q, label...)
	}
	q = append(q, 0, 0, 1, 0, 1)
	return q, nil
}

func skipName(msg []byte, off int) (int, error) {
	for {
		if off >= len(msg) {
			return 0, errDNSFailed
		}
		b := msg[off]
		switch {
		case b == 0:
			return off + 1, nil
		case b&0xC0 == 0xC0:
			if off+1 >= len(msg) {
				return 0, errDNSFailed
			}
			return off + 2, nil
		case b&0xC0 != 0:
			return 0, errDNSFailed
		default:
			off += 1 + int(b)
		}
	}
}

func parseResponse(msg, query []byte) ([4]byte, error) {
	var ip [4]byte
	if len(msg) < 12 {
		return ip, errDNSIgnore
	}
	flags := binary.BigEndian.Uint16(msg[2:4])
	if flags&0x8000 == 0 {
		return ip, errDNSIgnore
	}
	if binary.BigEndian.Uint16(msg[4:6]) != 1 || len(msg) < len(query) ||
		!bytes.EqualFold(msg[12:len(query)], query[12:]) {
		return ip, errDNSIgnore
	}
	if flags&0x000F != 0 {
		return ip, errDNSFailed
	}
	an := int(binary.BigEndian.Uint16(msg[6:8]))
	off := len(query)
	for i := 0; i < an; i++ {
		var err error
		off, err = skipName(msg, off)
		if err != nil || off+10 > len(msg) {
			return ip, errDNSFailed
		}
		typ := binary.BigEndian.Uint16(msg[off:])
		class := binary.BigEndian.Uint16(msg[off+2:])
		rdlen := int(binary.BigEndian.Uint16(msg[off+8:]))
		off += 10
		if off+rdlen > len(msg) {
			return ip, errDNSFailed
		}
		if typ == 1 && class == 1 && rdlen == 4 {
			copy(ip[:], msg[off:off+4])
			return ip, nil
		}
		off += rdlen
	}
	return ip, errDNSFailed
}

type lookup struct {
	id       uint16
	query    []byte
	attempts int
	timer    *Timer
	cb       func(ip [4]byte, err error)
}

type resolver struct {
	sel     *Selector
	fd      int
	server  *unix.SockaddrInet4
	pending map[uint16]*lookup
	buf     []byte
}

func newResolver(sel *Selector, server netip.AddrPort) (*resolver, error) {
	fd, err := unix.Socket(unix.AF_INET, unix.SOCK_DGRAM|unix.SOCK_NONBLOCK|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	r := &resolver{
		sel:     sel,
		fd:      fd,
		server:  &unix.SockaddrInet4{Port: int(server.Port()), Addr: server.Addr().As4()},
		pending: make(map[uint16]*lookup),
		buf:     make([]byte, 4096),
	}
	if err := sel.Add(fd, evIn, r); err != nil {
		unix.Close(fd)
		return nil, err
	}
	return r, nil
}

func (r *resolver) Resolve(name string, cb func([4]byte, error)) (*lookup, error) {
	if len(r.pending) >= dnsMaxPending {
		return nil, errDNSFailed
	}
	l := &lookup{cb: cb}
	for {
		l.id = uint16(rand.Uint32())
		if _, busy := r.pending[l.id]; !busy {
			break
		}
	}
	q, err := buildQuery(l.id, name)
	if err != nil {
		return nil, err
	}
	l.query = q
	r.pending[l.id] = l
	r.send(l)
	return l, nil
}

func (r *resolver) Cancel(l *lookup) {
	if r.pending[l.id] == l {
		delete(r.pending, l.id)
		r.sel.Cancel(l.timer)
	}
}

func (r *resolver) send(l *lookup) {
	l.attempts++
	if err := unix.Sendto(r.fd, l.query, 0, r.server); err != nil {
		log.Printf("dns: send: %v", err)
	}
	l.timer = r.sel.After(dnsTimeout, func() {
		r.onTimeout(l)
	})
}

func (r *resolver) onTimeout(l *lookup) {
	if r.pending[l.id] != l {
		return
	}
	if l.attempts >= dnsMaxAttempts {
		delete(r.pending, l.id)
		l.cb([4]byte{}, errDNSTimeout)
		return
	}
	r.send(l)
}

func (r *resolver) HandleEvent(fd int, ev uint32) {
	for i := 0; i < 64; i++ {
		n, from, err := unix.Recvfrom(r.fd, r.buf, 0)
		if err != nil {
			if err == unix.EINTR {
				continue
			}
			if err != unix.EAGAIN {
				log.Printf("dns: recv: %v", err)
			}
			return
		}
		r.handlePacket(r.buf[:n], from)
	}
}

func (r *resolver) handlePacket(msg []byte, from unix.Sockaddr) {
	sa, ok := from.(*unix.SockaddrInet4)
	if !ok || sa.Addr != r.server.Addr || sa.Port != r.server.Port || len(msg) < 2 {
		return
	}
	l := r.pending[binary.BigEndian.Uint16(msg)]
	if l == nil {
		return
	}
	ip, err := parseResponse(msg, l.query)
	if err == errDNSIgnore {
		return
	}
	delete(r.pending, l.id)
	r.sel.Cancel(l.timer)
	l.cb(ip, err)
}
