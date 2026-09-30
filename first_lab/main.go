package main

import (
	udp "first_lab/Multicast"
	"fmt"
	"net"
	"os"
	"strconv"
)

func main() {
	if len(os.Args) < 4 {
		fmt.Println("Не передан адрес multi-cast группы, название интерйейса или номер порта")
		return
	}

	ip := net.ParseIP(os.Args[1])
	if ip == nil || !ip.IsMulticast() {
		fmt.Printf("Неверный Multicast адрес %s", os.Args[1])
		return
	}

	port, err := strconv.Atoi(os.Args[3])
	if err != nil {
		fmt.Printf("Порт не является числом: %s", os.Args[3])
	}

	udp.Multicast(ip, os.Args[2], port)

}
