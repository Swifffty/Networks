package protocol

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"unicode/utf8"
)

const (
	MaxFileSize = 1 << 40
	MaxNameLen  = 4096
	SizeBuffer  = 1024
	SizeAnswer  = 4

	sizeLenData = 4
	sizeFileLen = 8
	sizeNameLen = 4

	offsetLenData = 0
	endLenData    = offsetLenData + sizeLenData // 4
	offsetFileLen = endLenData                  // 4
	endFileLen    = offsetFileLen + sizeFileLen // 12
	offsetNameLen = endFileLen                  // 12
	endNameLen    = offsetNameLen + sizeNameLen // 16

	MaxChunkData = SizeBuffer - endLenData

	answerCorrect = "OKEY"
	answerFail    = "FAIL"
)

func FirstProtSend(fileName string, sizeFile int64, file *os.File, buf []byte) (int, error) {
	if len(fileName) == 0 || len(fileName) > MaxNameLen {
		return 0, fmt.Errorf("недопустимая длина имени файла: %d байт", len(fileName))
	}
	binary.BigEndian.PutUint64(buf[offsetFileLen:endFileLen], uint64(sizeFile))
	binary.BigEndian.PutUint32(buf[offsetNameLen:endNameLen], uint32(len(fileName)))
	copied := copy(buf[endNameLen:], fileName)
	dataStart := endNameLen + copied

	n, err := file.Read(buf[dataStart:])
	binary.BigEndian.PutUint32(buf[offsetLenData:endLenData], uint32(n))
	return dataStart + n, err
}

func ProtSend(file *os.File, buf []byte) (int, error) {
	n, err := file.Read(buf[endLenData:])
	if n == 0 {
		return 0, err
	}
	binary.BigEndian.PutUint32(buf[offsetLenData:endLenData], uint32(n))
	return endLenData + n, err
}

func SendEnd(w io.Writer) error {
	var end [sizeLenData]byte
	_, err := w.Write(end[:])
	return err
}

func Check(buf []byte) bool {
	return len(buf) >= SizeAnswer && string(buf[:SizeAnswer]) == answerCorrect
}

func FirstProtReceive(r io.Reader) (dataLen uint32, size uint64, name string, err error) {
	var head [endNameLen]byte
	if _, err = io.ReadFull(r, head[:]); err != nil {
		return 0, 0, "", fmt.Errorf("чтение заголовка: %w", err)
	}
	dataLen = binary.BigEndian.Uint32(head[offsetLenData:endLenData])
	size = binary.BigEndian.Uint64(head[offsetFileLen:endFileLen])
	nameLen := binary.BigEndian.Uint32(head[offsetNameLen:endNameLen])

	if size > MaxFileSize {
		return 0, 0, "", fmt.Errorf("заявленный размер %d больше 1 ТБ", size)
	}
	if nameLen == 0 || nameLen > MaxNameLen {
		return 0, 0, "", fmt.Errorf("недопустимая длина имени: %d", nameLen)
	}
	if dataLen > MaxChunkData {
		return 0, 0, "", fmt.Errorf("недопустимая длина кадра: %d", dataLen)
	}

	nameBuf := make([]byte, nameLen)
	if _, err = io.ReadFull(r, nameBuf); err != nil {
		return 0, 0, "", fmt.Errorf("чтение имени: %w", err)
	}
	if !utf8.Valid(nameBuf) {
		return 0, 0, "", fmt.Errorf("имя файла не в UTF-8")
	}
	return dataLen, size, string(nameBuf), nil
}

func ProtReceiveLen(r io.Reader) (uint32, error) {
	var b [sizeLenData]byte
	if _, err := io.ReadFull(r, b[:]); err != nil {
		return 0, err
	}
	n := binary.BigEndian.Uint32(b[:])
	if n > MaxChunkData {
		return 0, fmt.Errorf("недопустимая длина кадра: %d", n)
	}
	return n, nil
}

func SendAnswer(w io.Writer, ok bool) error {
	answer := answerFail
	if ok {
		answer = answerCorrect
	}
	_, err := io.WriteString(w, answer)
	return err
}
