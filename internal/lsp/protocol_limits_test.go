package lsp

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"strings"
	"testing"
)

func TestReadMessageRejectsInvalidFraming(t *testing.T) {
	for _, tc := range []struct{ name, wire string }{
		{"unallocatable length", "Content-Length: 9223372036854775807\r\n\r\n"},
		{"over limit", "Content-Length: 16777217\r\n\r\n"},
		{"overflow", "Content-Length: 18446744073709551616\r\n\r\n"},
		{"negative", "Content-Length: -1\r\n\r\n"},
		{"duplicate", "Content-Length: 2\r\nContent-Length: 2\r\n\r\n{}"},
		{"conflicting duplicate", "Content-Length: 2\r\nContent-Length: 3\r\n\r\n{}"},
		{"missing length", "Content-Type: application/json\r\n\r\n{}"},
		{"long header line", "X-Extra: " + strings.Repeat("x", 8192) + "\r\nContent-Length: 2\r\n\r\n{}"},
		{"many headers", strings.Repeat("X-Extra: x\r\n", 1000) + "Content-Length: 2\r\n\r\n{}"},
		{"truncated header", "Content-Length: 2\r\n"},
		{"truncated body", "Content-Length: 2\r\n\r\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if p := recover(); p != nil {
					t.Errorf("invalid framing panicked: %v", p)
				}
			}()
			_, err := readMessage(bufio.NewReader(strings.NewReader(tc.wire)))
			if err == nil || err == io.EOF {
				t.Fatalf("invalid framing must return a non-EOF error, got %v", err)
			}
		})
	}
}

func TestReadMessageBoundariesAndSmallReader(t *testing.T) {
	for _, size := range []int{2, 16 * 1024 * 1024} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			body := bytes.Repeat([]byte("x"), size)
			wire := append([]byte(fmt.Sprintf("Content-Length: %d\r\nContent-Type: application/json\r\n\r\n", size)), body...)
			reader := bufio.NewReaderSize(bytes.NewReader(wire), 16)
			got, err := readMessage(reader)
			if err != nil || !bytes.Equal(got, body) {
				t.Fatalf("read %d-byte message: length=%d, error=%v", size, len(got), err)
			}
		})
	}
}
