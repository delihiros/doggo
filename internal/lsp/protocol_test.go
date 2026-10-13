package lsp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"testing"
)

func TestWriteReadMessageRoundTrip(t *testing.T) {
	var wire bytes.Buffer
	bw := bufio.NewWriter(&wire)

	msg := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
	}
	if err := writeMessage(bw, msg); err != nil {
		t.Fatalf("writeMessage failed: %v", err)
	}

	br := bufio.NewReader(bytes.NewReader(wire.Bytes()))
	payload, err := readMessage(br)
	if err != nil {
		t.Fatalf("readMessage failed: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("invalid json payload: %v", err)
	}
	if decoded["method"] != "initialize" {
		t.Fatalf("method mismatch: %v", decoded["method"])
	}
}

func TestReadMessageEOF(t *testing.T) {
	br := bufio.NewReader(bytes.NewReader(nil))
	_, err := readMessage(br)
	if err == nil {
		t.Fatal("expected EOF error")
	}
	if err != io.EOF {
		t.Fatalf("expected io.EOF, got %v", err)
	}
}
