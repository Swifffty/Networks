package udp

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
	"golang.org/x/net/ipv6"
	"golang.org/x/net/ipv4"
	"golang.org/x/sys/unix"
	"github.com/google/uuid"
)

var magic = [4]byte{'D', 'L', 'A', 'B'}

const msgLen = 4 + 16

type multicastConn interface {
	JoinGroup(ifi *net.Interface, group net.Addr) error
	SetMulticastInterface(ifi *net.Interface) error
}

type Event struct {
	addr       net.Addr
	timeRecive time.Time
	id         uuid.UUID
}

type Info struct {
	addr       net.Addr
	timeRecive time.Time
}

type Node struct {
	conn      net.PacketConn
	iface     *net.Interface
	groupAddr *net.UDPAddr
	events    chan Event
	selfID    uuid.UUID
}

func (node *Node) Tracker(ctx context.Context) {
	alive := make(map[uuid.UUID]Info)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case e := <-node.events:
			alive[e.id] = Info{addr: e.addr, timeRecive: e.timeRecive}
			fmt.Println("Устройства с копией:")
			for _, device := range alive {
				fmt.Println(device.addr)
			}
		case <-ticker.C:
			flag := false
			for id, device := range alive {
				if time.Since(device.timeRecive) >= 5*time.Second {
					flag = true
					delete(alive, id)
				}
			}
			if flag {
				fmt.Println("Обновленный список")
				for _, device := range alive {
					fmt.Println(device.addr)
				}
			}
		case <-ctx.Done():
			return
		}
	}
}

func (node *Node) Recive() {
	buf := make([]byte, msgLen)
	for {
		n, addr, err := node.conn.ReadFrom(buf)
		if err != nil {
			return
		}
		if n != msgLen {
			continue
		}
		if !bytes.Equal(buf[:4], magic[:]) {
			continue
		}
		var id uuid.UUID
		copy(id[:], buf[4:msgLen])
		if id != node.selfID {
			node.events <- Event{addr: addr, timeRecive: time.Now(), id: id}
		}
	}
}

func (node *Node) Send(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	buf := make([]byte, 0, msgLen)
	buf = append(buf, magic[:]...)
	buf = append(buf, node.selfID[:]...)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			_, err := node.conn.WriteTo(buf, node.groupAddr)
			if err != nil {
				log.Printf("Ошибка отправления heartbeat: %v", err)
			}
		case <-ctx.Done():
			return
		}
	}

}

func Multicast(ip net.IP, ifaceName string, port int) {
	selfUid := uuid.New()
	lc := net.ListenConfig{
		Control: func(network, address string, c syscall.RawConn) error {
			var errorSocket error
			err := c.Control(func(fd uintptr) {
				errorSocket = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_REUSEADDR, 1)
			})

			if err != nil {
				return err
			}
			return errorSocket
		},
	}

    var conn net.PacketConn
	var ipConn multicastConn
	var destAddr *net.UDPAddr
	var err error

	iface, err := net.InterfaceByName(ifaceName)
	if err != nil {
		log.Fatal("Не найден интерфейс")
	}

	if (ip.To4() != nil) {
	conn, err = lc.ListenPacket(context.Background(), "udp4", fmt.Sprintf(":%d", port))
	if err != nil {
		log.Fatal("Ошибка при создании сокета")
		}
		ipConn = ipv4.NewPacketConn(conn)
		destAddr = &net.UDPAddr{IP: ip, Port: port}
	} else {
			conn, err = lc.ListenPacket(context.Background(), "udp6", fmt.Sprintf(":%d", port))
		if err != nil {
			log.Fatal("Ошибка при создании сокета")
		}
			ipConn = ipv6.NewPacketConn(conn)
			destAddr = &net.UDPAddr{IP: ip, Port: port, Zone : ifaceName}
		}    


	groupAddr := &net.UDPAddr{IP: ip}

	err = ipConn.JoinGroup(iface, groupAddr)
	if err != nil {
		log.Fatal("Не удалось подключиться к группе")
	}

	err = ipConn.SetMulticastInterface(iface)
	if (err != nil) {
		log.Fatal("Не удалось настроить заданный интерфейс: ", err)
		return
	}

	events := make(chan Event, 4)
	node := Node{conn: conn, iface: iface, groupAddr: destAddr, events: events, selfID: selfUid}
	ctx, cancel := context.WithCancel(context.Background())

	var wg sync.WaitGroup
	wg.Add(3)

	go func() { defer wg.Done(); node.Recive() }()
	go func() { defer wg.Done(); node.Send(ctx) }()
	go func() { defer wg.Done(); node.Tracker(ctx) }()

	sigCh := make(chan os.Signal, 1)

	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	<-sigCh

	cancel()
	conn.Close()
	wg.Wait()
}