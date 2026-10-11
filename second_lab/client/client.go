package client

import (
	"log"
	"net"
	"os"
	"strconv"
	"path/filepath"
	"encoding/binary"
	"io"
)

const maxFileSize = 1 << 40

func send_file(conn net.Conn, pathFile string) {
	file, err := os.Open(pathFile)
	if (err != nil) {
		log.Fatalf("Ошибка открытия файла %v\n", err)
	}
	defer file.Close()

	fileName := filepath.Base(pathFile) // нужно как то выделить протокол так, чтобы не приходилось изменять точечно в функциях особенности протокола
	buf := make([]byte, 64 * 1024)
	binary.BigEndian.PutUint32(buf[4:8], uint32(len(fileName)))
	copied := copy(buf[8:], fileName)
	n, err := file.Read(buf[8 + copied:])
	
	switch {
	case n > 0:
		binary.BigEndian.PutUint32(buf[0:4], uint32(n))
			sent, err := conn.Write(buf)
			if (err != nil || sent != len(buf)) {
				log.Fatal("Ошибка отправки данных на сервер")
			}
	case err == io.EOF:
		return
	default:
		log.Fatalf("Ошибка отправки данных на сервер %v\n", )
	}

	if (err == io.EOF) {
		return
	}

	for {
		n, err = file.Read(buf[4:])
		if (n > 0) {
			binary.BigEndian.PutUint32(buf[0:4], uint32(n))
			sent, err := conn.Write(buf)
			if (err != nil || sent != len(buf)) {
				log.Fatal("Ошибка отправки данных на сервер")
			}
		}
		if (err == io.EOF) {
			return
		}

	}
	
	

}

func main() {
	if (len(os.Args) < 4) {
		log.Fatal("Переданно недостаточное количество параметров")
	}

	info, err := os.Stat(os.Args[1])
	if (err != nil) {
		log.Fatalf("Ошибка получения информации о файле: %v\n", err)
	}
	if (!info.Mode().IsRegular()) {
		log.Fatal("Файл не является регулярным\n")
	} 

	if (info.Size() > maxFileSize) {
		log.Fatal("Слишком большой размер файла\n")
	}

	port, err := strconv.Atoi(os.Args[3])
	if (err != nil || port < 1 || port > 65535) {
		log.Fatal("Введен некорректный порт\n")
	}

	addr := net.JoinHostPort(os.Args[2], os.Args[3])
	conn, err := net.Dial("tcp", addr)
	if (err != nil) {
		log.Fatalf("Ошибка подключения по адресу %s\n", addr)
	}
	defer conn.Close()
	
	

}