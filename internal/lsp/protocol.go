package lsp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
)

const (
	jsonRPCVersion     = "2.0"
	maxLSPHeaderBytes  = 8 * 1024
	maxLSPMessageBytes = 16 * 1024 * 1024
)

type message struct {
	JSONRPC string           `json:"jsonrpc,omitempty"`
	ID      *json.RawMessage `json:"id,omitempty"`
	Method  string           `json:"method,omitempty"`
	Params  json.RawMessage  `json:"params,omitempty"`
	Result  any              `json:"result,omitempty"`
	Error   *responseError   `json:"error,omitempty"`
}

type responseError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func readMessage(r *bufio.Reader) ([]byte, error) {
	contentLength := -1
	headerBytes := 0
	for {
		lineBytes, err := readHeaderLine(r, maxLSPHeaderBytes-headerBytes)
		if err != nil {
			if err == io.EOF && headerBytes > 0 {
				return nil, io.ErrUnexpectedEOF
			}
			return nil, err
		}
		headerBytes += len(lineBytes)
		line := strings.TrimRight(string(lineBytes), "\r\n")
		if line == "" {
			break
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid LSP header")
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		if strings.EqualFold(key, "Content-Length") {
			if contentLength >= 0 {
				return nil, fmt.Errorf("duplicate Content-Length header")
			}
			n, err := strconv.Atoi(value)
			if err != nil || n < 0 || n > maxLSPMessageBytes {
				return nil, fmt.Errorf("Content-Length must be between 0 and %d bytes", maxLSPMessageBytes)
			}
			contentLength = n
		}
	}

	if contentLength < 0 {
		return nil, fmt.Errorf("missing Content-Length header")
	}

	payload := make([]byte, contentLength)
	if _, err := io.ReadFull(r, payload); err != nil {
		if err == io.EOF {
			err = io.ErrUnexpectedEOF
		}
		return nil, err
	}
	return payload, nil
}

// Bound header storage even when a peer never sends a newline. ReadSlice may
// fragment a valid line when the caller supplies a small buffered reader.
func readHeaderLine(r *bufio.Reader, remaining int) ([]byte, error) {
	var line []byte
	for {
		fragment, err := r.ReadSlice('\n')
		if len(fragment) > remaining-len(line) {
			return nil, fmt.Errorf("LSP headers exceed %d bytes", maxLSPHeaderBytes)
		}
		line = append(line, fragment...)
		if err == bufio.ErrBufferFull {
			continue
		}
		if err == io.EOF && len(line) > 0 {
			return nil, io.ErrUnexpectedEOF
		}
		return line, err
	}
}

func writeMessage(w *bufio.Writer, msg any) error {
	body, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Content-Length: %d\r\n\r\n", len(body)); err != nil {
		return err
	}
	if _, err := w.Write(body); err != nil {
		return err
	}
	return w.Flush()
}
