package main

import (
	"fmt"
	"log"
	"net/netip"
	"os"
	"strconv"

	"SOCKS5_lab/server"
)

func main() {
	if len(os.Args) < 2 || len(os.Args) > 3 {
		fmt.Fprintf(os.Stderr, "usage: %s <port> [dns_ipv4:port]\n", os.Args[0])
		os.Exit(2)
	}

	port, err := strconv.Atoi(os.Args[1])
	if err != nil || port < 1 || port > 65535 {
		log.Fatalf("bad port %q", os.Args[1])
	}

	var dns netip.AddrPort
	if len(os.Args) == 3 {
		dns, err = netip.ParseAddrPort(os.Args[2])
		if err != nil {
			log.Fatalf("bad DNS resolver %q (want IPv4:port): %v", os.Args[2], err)
		}
	} else {
		dns, err = server.SystemResolver()
		if err != nil {
			log.Fatalf("system resolver: %v", err)
		}
	}

	srv, err := server.New(port, dns)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("SOCKS5 proxy on :%d, DNS resolver %v", port, dns)
	log.Fatal(srv.Run())
}
