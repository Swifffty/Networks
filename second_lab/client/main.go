package main

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"

	"second_lab/protocol"
)

func sendFile(conn net.Conn, file *os.File, fileName string, sizeFile int64) error {
	buf := make([]byte, protocol.SizeBuffer)
	n, err := protocol.FirstProtSend(fileName, sizeFile, file, buf)
	if err != nil && err != io.EOF {
		return err
	}
	if _, werr := conn.Write(buf[:n]); werr != nil {
		return fmt.Errorf("отправка данных: %w", werr)
	}

	for err != io.EOF {
		n, err = protocol.ProtSend(file, buf)
		if err != nil && err != io.EOF {
			return fmt.Errorf("чтение файла: %w", err)
		}
		if n > 0 {
			if _, werr := conn.Write(buf[:n]); werr != nil {
				return fmt.Errorf("отправка данных: %w", werr)
			}
		}
	}

	if err := protocol.SendEnd(conn); err != nil {
		return fmt.Errorf("отправка конца файла: %w", err)
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
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return fmt.Errorf("подключение к %s: %w", addr, err)
	}
	defer conn.Close()

	if err := sendFile(conn, file, filepath.Base(path), info.Size()); err != nil {
		return err
	}

	answer := make([]byte, protocol.SizeAnswer)
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
