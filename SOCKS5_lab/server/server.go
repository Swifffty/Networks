package server

import (
	"fmt"
	"log"
	"net/netip"
	"time"

	"golang.org/x/sys/unix"
)

const acceptPause = 100 * time.Millisecond

type Server struct {
	sel *Selector
	lfd int
	dns *resolver
}

func New(port int, dnsAddr netip.AddrPort) (*Server, error) {
	ip := dnsAddr.Addr().Unmap()
	if !ip.Is4() {
		return nil, fmt.Errorf("DNS resolver must be IPv4, got %v", dnsAddr)
	}
	sel, err := NewSelector()
	if err != nil {
		return nil, err
	}
	dns, err := newResolver(sel, netip.AddrPortFrom(ip, dnsAddr.Port()))
	if err != nil {
		return nil, err
	}

	lfd, err := unix.Socket(unix.AF_INET, unix.SOCK_STREAM|unix.SOCK_NONBLOCK|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	if err := unix.SetsockoptInt(lfd, unix.SOL_SOCKET, unix.SO_REUSEADDR, 1); err != nil {
		unix.Close(lfd)
		return nil, err
	}
	if err := unix.Bind(lfd, &unix.SockaddrInet4{Port: port}); err != nil {
		unix.Close(lfd)
		return nil, fmt.Errorf("bind: %w", err)
	}
	if err := unix.Listen(lfd, unix.SOMAXCONN); err != nil {
		unix.Close(lfd)
		return nil, err
	}

	s := &Server{sel: sel, lfd: lfd, dns: dns}
	if err := sel.Add(lfd, evIn, s); err != nil {
		unix.Close(lfd)
		return nil, err
	}
	return s, nil
}

func (s *Server) Run() error {
	return s.sel.Run()
}

func (s *Server) HandleEvent(fd int, ev uint32) {
	for i := 0; i < 128; i++ {
		nfd, _, err := unix.Accept4(s.lfd, unix.SOCK_NONBLOCK|unix.SOCK_CLOEXEC)
		if err != nil {
			switch err {
			case unix.EAGAIN:
				return
			case unix.EINTR, unix.ECONNABORTED:
				continue
			default:
				log.Printf("accept: %v (pause %v)", err, acceptPause)
				s.pauseAccept()
				return
			}
		}
		s.newConn(nfd)
	}
}

func (s *Server) pauseAccept() {
	s.sel.Remove(s.lfd)
	s.sel.After(acceptPause, func() {
		if err := s.sel.Add(s.lfd, evIn, s); err != nil {
			log.Printf("re-add listener: %v", err)
		}
	})
}
