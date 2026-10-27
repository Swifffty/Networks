package main

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"second_lab/protocol"
)

const (
	uploadDir     = "uploads"
	speedInterval = 3 * time.Second
	maxStoredName = 200
	toMb          = 1024 * 1024
	maxClients    = 16
)

type speedMeter struct {
	client   string
	start    time.Time
	total    atomic.Int64
	done     chan struct{}
	finished chan struct{}
}

func startSpeedMeter(client string) *speedMeter {
	m := &speedMeter{
		client:   client,
		start:    time.Now(),
		done:     make(chan struct{}),
		finished: make(chan struct{}),
	}
	go m.run()
	return m
}

func (m *speedMeter) add(n int64) { m.total.Add(n) }

func (m *speedMeter) stop() {
	close(m.done)
	<-m.finished
}

func (m *speedMeter) run() {
	defer close(m.finished)
	ticker := time.NewTicker(speedInterval)
	defer ticker.Stop()

	lastTime := m.start
	var lastTotal int64
	printed := false

	for {
		select {
		case now := <-ticker.C:
			total := m.total.Load()
			m.print(total-lastTotal, now.Sub(lastTime), total, now.Sub(m.start))
			lastTime, lastTotal = now, total
			printed = true
		case <-m.done:
			if !printed {
				elapsed := time.Since(m.start)
				total := m.total.Load()
				m.print(total, elapsed, total, elapsed)
			}
			return
		}
	}
}

func (m *speedMeter) print(delta int64, deltaTime time.Duration, total int64, elapsed time.Duration) {
	log.Printf("%s: мгновенная %12s, средняя %12s, принято %11s",
		m.client, formatSpeed(delta, deltaTime), formatSpeed(total, elapsed), formatBytes(float64(total)))
}

func formatSpeed(bytes int64, d time.Duration) string {
	sec := d.Seconds()
	if sec <= 0 {
		return "—"
	}
	return formatBytes(float64(bytes)/sec) + "/с"
}

func formatBytes(n float64) string {
	return fmt.Sprintf("%.1f МБ", n/toMb)
}

type result struct {
	name     string
	path     string
	received uint64
}

func handleClient(conn net.Conn, id string) {
	defer conn.Close()

	defer func() {
		if r := recover(); r != nil {
			log.Printf("%s внутренняя ошибка: %v", id, r)
		}
	}()

	res, err := receiveFile(conn, id)

	conn.SetWriteDeadline(time.Now().Add(protocol.IoTimeout))
	werr := protocol.SendAnswer(conn, err == nil)

	switch {
	case err == nil && werr == nil:
		log.Printf("%s файл %s принят, сохранён как %s", id, res.name, res.path)
	case err == nil:
		log.Printf("%s файл %s сохранён как %s, но не удалось отправить ответ: %v", id, res.name, res.path, werr)
	case res.name != "":
		log.Printf("%s ошибка при приёме %s: %v", id, res.name, err)
	default:
		log.Printf("%s %s ошибка: %v", id, conn.RemoteAddr(), err)
	}
}

func receiveFile(conn net.Conn, id string) (res result, err error) {
	conn.SetReadDeadline(time.Now().Add(protocol.IoTimeout))
	name, size, err := protocol.ReadHeader(conn)
	if err != nil {
		return res, err
	}
	res.name = name
	log.Printf("%s %s передаёт %s (%s)", id, conn.RemoteAddr(), name, formatBytes(float64(size)))
	if err := checkName(name); err != nil {
		return res, err
	}

	f, path, err := createUnique(uploadDir, name)
	if err != nil {
		return res, fmt.Errorf("создание файла: %w", err)
	}
	res.path = path

	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("закрытие файла: %w", cerr)
		}
		if err != nil {
			os.Remove(path)
		}
	}()

	meter := startSpeedMeter(id + " " + name)
	defer meter.stop()

	buf := make([]byte, protocol.SizeBuffer)
	for res.received < size {
		chunk := uint64(len(buf))
		if left := size - res.received; left < chunk {
			chunk = left
		}
		conn.SetReadDeadline(time.Now().Add(protocol.IoTimeout))
		n, rerr := conn.Read(buf[:chunk])
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				return res, fmt.Errorf("запись в файл: %w", werr)
			}
			res.received += uint64(n)
			meter.add(int64(n))
		}
		if errors.Is(rerr, io.EOF) {
			return res, fmt.Errorf("получено %d байт, а заявлено %d", res.received, size)
		}
		if rerr != nil {
			return res, fmt.Errorf("чтение данных: %w", rerr)
		}
	}
	return res, nil
}

func checkName(name string) error {
	if name == "." || name == ".." {
		return fmt.Errorf("недопустимое имя файла: %q", name)
	}
	if strings.ContainsAny(name, "/\\\x00") {
		return fmt.Errorf("имя файла содержит недопустимые символы: %q", name)
	}
	return nil
}

func createUnique(dir, name string) (*os.File, string, error) {
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)

	for i := 0; i < 10000; i++ {
		candidate := name
		if i > 0 {
			candidate = fmt.Sprintf("%s (%d)%s", base, i, ext)
		}
		path := filepath.Join(dir, candidate)
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err == nil {
			return f, path, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, "", err
		}
	}
	return nil, "", errors.New("не удалось подобрать свободное имя")
}

func main() {
	if len(os.Args) != 2 {
		log.Fatalf("Использование: %s <порт>", os.Args[0])
	}
	port, err := strconv.Atoi(os.Args[1])
	if err != nil || port < 1 || port > 65535 {
		log.Fatal("Некорректный порт")
	}
	if err := os.MkdirAll(uploadDir, 0o755); err != nil {
		log.Fatalf("Не удалось создать каталог %s: %v", uploadDir, err)
	}

	log.SetFlags(0)

	ln, err := net.Listen("tcp", ":"+os.Args[1])
	if err != nil {
		log.Fatalf("Ошибка создания сокета: %v", err)
	}
	defer ln.Close()
	log.Printf("Сервер слушает порт %d", port)

	sem := make(chan struct{}, maxClients)
	clientNum := 0
	var delay time.Duration
	for {
		conn, err := ln.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				log.Printf("Сокет сервера закрыт")
				return
			}
			if delay == 0 {
				delay = 5 * time.Millisecond
			} else {
				delay *= 2
			}
			if delay > time.Second {
				delay = time.Second
			}
			log.Printf("Ошибка принятия подключения: %v, повтор через %v", err, delay)
			time.Sleep(delay)
			continue
		}
		delay = 0
		clientNum++
		id := fmt.Sprintf("#%d", clientNum)

		select {
		case sem <- struct{}{}:
			go func() {
				defer func() { <-sem }()
				handleClient(conn, id)
			}()
		default:
			log.Printf("%s %s отклонён: достигнут лимит %d клиентов", id, conn.RemoteAddr(), maxClients)
			conn.SetWriteDeadline(time.Now().Add(protocol.IoTimeout))
			protocol.SendAnswer(conn, false)
			conn.Close()
		}
	}
}
