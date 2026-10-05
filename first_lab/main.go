package main

import (
	udp "first_lab/Multicast"
	"fmt"
	"net"
	"os"
	"strconv"
)

func main() {
	if len(os.Args) < 3 {
		fmt.Println("Не передан адрес multi-cast группы или название интерйейса")
		return
	}


	ip := net.ParseIP(os.Args[1])
	if ip == nil || !ip.IsMulticast() {
		fmt.Printf("Неверный Multicast адрес %s", os.Args[1])
		return
	}

	var port int

	if(len(os.Args) == 3) {
		port = 9999;
	} else {
		port, err := strconv.Atoi(os.Args[3])
		if (err != nil || port < 1 || port > 65535) {
			fmt.Printf("Недопустимое значение порта: %s", os.Args[3])
			return
		}
	}



	udp.Multicast(ip, os.Args[2], port)

}
