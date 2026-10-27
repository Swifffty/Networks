package protocol

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"time"
	"unicode/utf8"
)

const (
	MaxFileSize = 1 << 40
	MaxNameLen  = 4096
	SizeBuffer  = 4096 * 2
	SizeAnswer  = 1
	IoTimeout   = 30 * time.Second

	magic       = "PROT"
	sizeMagic   = len(magic)
	sizeNameLen = 2
	sizeFileLen = 8
	prefixSize  = sizeMagic + sizeNameLen

	statusOK   byte = 0
	statusFail byte = 1
)

func validateName(name string) error {
	if len(name) == 0 || len(name) > MaxNameLen {
		return fmt.Errorf("длина имени файла должна быть от 1 до %d байт, получено %d", MaxNameLen, len(name))
	}
	if !utf8.ValidString(name) {
		return errors.New("имя файла не является корректной строкой UTF-8")
	}
	return nil
}

func validateSize(size uint64) error {
	if size > MaxFileSize {
		return fmt.Errorf("размер файла %d байт превышает 1 ТБ", size)
	}
	return nil
}

func WriteHeader(w io.Writer, name string, size uint64) error {
	if err := validateName(name); err != nil {
		return err
	}
	if err := validateSize(size); err != nil {
		return err
	}

	buf := make([]byte, prefixSize+len(name)+sizeFileLen)
	copy(buf, magic)
	binary.BigEndian.PutUint16(buf[sizeMagic:prefixSize], uint16(len(name)))
	copy(buf[prefixSize:], name)
	binary.BigEndian.PutUint64(buf[prefixSize+len(name):], size)

	_, err := w.Write(buf)
	return err
}

func ReadHeader(r io.Reader) (name string, size uint64, err error) {
	var prefix [prefixSize]byte
	if _, err = io.ReadFull(r, prefix[:]); err != nil {
		return "", 0, fmt.Errorf("чтение заголовка: %w", err)
	}
	if string(prefix[:sizeMagic]) != magic {
		return "", 0, errors.New("неверная сигнатура протокола")
	}
	nameLen := binary.BigEndian.Uint16(prefix[sizeMagic:])
	if nameLen == 0 || int(nameLen) > MaxNameLen {
		return "", 0, fmt.Errorf("неверная длина имени файла: %d", nameLen)
	}

	nameBuf := make([]byte, nameLen)
	if _, err = io.ReadFull(r, nameBuf); err != nil {
		return "", 0, fmt.Errorf("чтение имени: %w", err)
	}
	name = string(nameBuf)
	if err = validateName(name); err != nil {
		return "", 0, err
	}

	var sizeBuf [sizeFileLen]byte
	if _, err = io.ReadFull(r, sizeBuf[:]); err != nil {
		return "", 0, fmt.Errorf("чтение размера: %w", err)
	}
	size = binary.BigEndian.Uint64(sizeBuf[:])
	if err = validateSize(size); err != nil {
		return "", 0, err
	}
	return name, size, nil
}

func SendAnswer(w io.Writer, ok bool) error {
	status := statusFail
	if ok {
		status = statusOK
	}
	_, err := w.Write([]byte{status})
	return err
}

func Check(buf []byte) bool {
	return len(buf) == SizeAnswer && buf[0] == statusOK
}
