package main

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"second_lab/protocol"
	"strconv"
	"time"
)

func sendFile(conn net.Conn, file *os.File, fileName string, sizeFile int64) error {
	conn.SetWriteDeadline(time.Now().Add(protocol.IoTimeout))
	if err := protocol.WriteHeader(conn, fileName, uint64(sizeFile)); err != nil {
		return fmt.Errorf("отправка заголовка: %w", err)
	}

	buf := make([]byte, protocol.SizeBuffer)
	var sent int64
	for sent < sizeFile {
		chunk := int64(len(buf))
		if left := sizeFile - sent; left < chunk {
			chunk = left
		}
		n, err := file.Read(buf[:chunk])
		if n > 0 {
			conn.SetWriteDeadline(time.Now().Add(protocol.IoTimeout))
			if _, werr := conn.Write(buf[:n]); werr != nil {
				return fmt.Errorf("отправка данных: %w", werr)
			}
			sent += int64(n)
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("чтение файла: %w", err)
		}
	}

	if sent != sizeFile {
		return fmt.Errorf("файл изменился во время отправки: отправлено %d из %d байт", sent, sizeFile)
	}
	return nil
}

func run() error {
	if len(os.Args) != 4 {
		return fmt.Errorf("использование: %s <файл> <хост> <порт>", os.Args[0])
	}
	path, host, portStr := os.Args[1], os.Args[2], os.Args[3]

	port, err := strconv.Atoi(portStr)
	if err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("некорректный порт: %s", portStr)
	}

	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("открытие файла: %w", err)
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("информация о файле: %w", err)
	}
	if !info.Mode().IsRegular() {
		return errors.New("файл не является обычным файлом")
	}
	if info.Size() > protocol.MaxFileSize {
		return errors.New("файл больше 1 ТБ")
	}

	addr := net.JoinHostPort(host, portStr)
	conn, err := net.DialTimeout("tcp", addr, protocol.IoTimeout)
	if err != nil {
		return fmt.Errorf("подключение к %s: %w", addr, err)
	}
	defer conn.Close()

	if err := sendFile(conn, file, filepath.Base(path), info.Size()); err != nil {
		return err
	}
	if err := conn.(*net.TCPConn).CloseWrite(); err != nil {
		return fmt.Errorf("завершение отправки: %w", err)
	}
	answer := make([]byte, protocol.SizeAnswer)

	conn.SetReadDeadline(time.Now().Add(protocol.IoTimeout))
	if _, err := io.ReadFull(conn, answer); err != nil {
		return fmt.Errorf("получение ответа от сервера: %w", err)
	}
	if !protocol.Check(answer) {
		return errors.New("сервер сообщил об ошибке")
	}
	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Println("Не удалось передать файл:", err)
		os.Exit(1)
	}
	fmt.Println("Успех! Файл передан.")
}
