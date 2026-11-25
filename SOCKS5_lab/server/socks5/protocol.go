package socks5

import (
	"encoding/binary"
	"errors"
)

const (
	Version5 = 0x05
)

const (
	MethodNoAuth       = 0x00
	MethodNoAcceptable = 0xFF
)

const (
	CmdConnect = 0x01
)

const (
	AtypIPv4   = 0x01
	AtypDomain = 0x03
	AtypIPv6   = 0x04
)

const (
	RepSucceeded        = 0x00
	RepGeneralFailure   = 0x01
	RepNotAllowed       = 0x02
	RepNetUnreachable   = 0x03
	RepHostUnreachable  = 0x04
	RepConnRefused      = 0x05
	RepTTLExpired       = 0x06
	RepCmdNotSupported  = 0x07
	RepAtypNotSupported = 0x08
)

const (
	MaxGreetingLen = 2 + 255
	MaxRequestLen  = 4 + 1 + 255 + 2
	replyLen       = 10
)

var (
	ErrBadVersion       = errors.New("socks5: unsupported protocol version")
	ErrMalformed        = errors.New("socks5: malformed message")
	ErrCmdNotSupported  = errors.New("socks5: command not supported")
	ErrAtypNotSupported = errors.New("socks5: address type not supported")
)

func ParseGreeting(b []byte) (n int, noAuth bool, err error) {
	if len(b) < 2 {
		return 0, false, nil
	}
	if b[0] != Version5 {
		return 0, false, ErrBadVersion
	}
	nm := int(b[1])
	if len(b) < 2+nm {
		return 0, false, nil
	}
	for _, m := range b[2 : 2+nm] {
		if m == MethodNoAuth {
			noAuth = true
		}
	}
	return 2 + nm, noAuth, nil
}

type Request struct {
	Atyp   byte
	IP     [4]byte
	Domain string
	Port   uint16
}

func ParseRequest(b []byte) (req Request, n int, err error) {
	if len(b) < 4 {
		return req, 0, nil
	}
	if b[0] != Version5 {
		return req, 0, ErrBadVersion
	}
	if b[1] != CmdConnect {
		return req, 0, ErrCmdNotSupported
	}
	req.Atyp = b[3]
	switch b[3] {
	case AtypIPv4:
		if len(b) < 4+4+2 {
			return req, 0, nil
		}
		copy(req.IP[:], b[4:8])
		req.Port = binary.BigEndian.Uint16(b[8:10])
		return req, 10, nil
	case AtypDomain:
		if len(b) < 5 {
			return req, 0, nil
		}
		l := int(b[4])
		if l == 0 {
			return req, 0, ErrMalformed
		}
		total := 5 + l + 2
		if len(b) < total {
			return req, 0, nil
		}
		req.Domain = string(b[5 : 5+l])
		req.Port = binary.BigEndian.Uint16(b[5+l : total])
		return req, total, nil
	default:
		return req, 0, ErrAtypNotSupported
	}
}

func BuildReply(rep byte, ip [4]byte, port uint16) []byte {
	r := make([]byte, replyLen)
	r[0] = Version5
	r[1] = rep
	r[2] = 0
	r[3] = AtypIPv4
	copy(r[4:8], ip[:])
	binary.BigEndian.PutUint16(r[8:10], port)
	return r
}
