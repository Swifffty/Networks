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
	log.Printf("[%s] мгновенная: %s, средняя: %s, принято: %s",
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
	return fmt.Sprintf("%.2f МБ", n/toMb)
}

func handleClient(conn net.Conn) {
	defer conn.Close()
	client := conn.RemoteAddr().String()
	log.Printf("[%s] подключился", client)

	defer func() {
		if r := recover(); r != nil {
			log.Printf("[%s] внутренняя ошибка: %v", client, r)
		}
	}()

	err := receiveFile(conn, client)
	if err != nil {
		log.Printf("[%s] ошибка: %v", client, err)
	} else {
		log.Printf("[%s] файл принят успешно", client)
	}

	if werr := protocol.SendAnswer(conn, err == nil); werr != nil {
		log.Printf("[%s] не удалось отправить ответ: %v", client, werr)
	}
}

func receiveFile(conn net.Conn, client string) (err error) {
	dataLen, size, name, err := protocol.FirstProtReceive(conn)
	if err != nil {
		return err
	}
	if err := checkName(name); err != nil {
		return err
	}

	f, path, err := createUnique(uploadDir, name)

	if err != nil {
		return fmt.Errorf("создание файла: %w", err)
	}
	log.Printf("[%s] Принмаю файл %s", client, name)

	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("закрытие файла: %w", cerr)
		}
		if err != nil {
			os.Remove(path)
		}
	}()

	meter := startSpeedMeter(client)
	defer meter.stop()

	buf := make([]byte, protocol.MaxChunkData)
	var received uint64
	for {
		if received+uint64(dataLen) > size {
			return fmt.Errorf("клиент прислал больше заявленных %d байт", size)
		}
		if _, err := io.ReadFull(conn, buf[:dataLen]); err != nil {
			return fmt.Errorf("чтение данных: %w", err)
		}
		if _, err := f.Write(buf[:dataLen]); err != nil {
			return fmt.Errorf("запись в файл: %w", err)
		}
		received += uint64(dataLen)
		meter.add(int64(dataLen))

		dataLen, err = protocol.ProtReceiveLen(conn)
		if err != nil {
			return fmt.Errorf("чтение длины кадра: %w", err)
		}
		if dataLen == 0 {
			break
		}
	}

	if received != size {
		return fmt.Errorf("получено %d байт, а заявлено %d", received, size)
	}
	return nil
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

	ln, err := net.Listen("tcp", ":"+os.Args[1])
	if err != nil {
		log.Fatalf("Ошибка создания сокета: %v", err)
	}
	defer ln.Close()
	log.Printf("Сервер слушает порт %d", port)

	sem := make(chan struct{}, maxClients)
	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Printf("Ошибка принятия подключения: %v", err)
			continue
		}

		select {
		case sem <- struct{}{}:
			go func() {
				defer func() { <-sem }()
				handleClient(conn)
			}()
		default:
			log.Printf("[%s] отклонён: достигнут лимит %d клиентов", conn.RemoteAddr(), maxClients)
			protocol.SendAnswer(conn, false)
			conn.Close()
		}
	}
}
