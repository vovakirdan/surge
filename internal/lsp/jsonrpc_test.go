package lsp

import (
	"bufio"
	"bytes"
	"testing"
)

func TestJSONRPCFramingMultipleMessages(t *testing.T) {
	var buf bytes.Buffer
	msg1 := []byte(`{"jsonrpc":"2.0","method":"one"}`)
	msg2 := []byte(`{"jsonrpc":"2.0","method":"two"}`)

	if err := writeMessage(&buf, msg1); err != nil {
		t.Fatalf("write message 1: %v", err)
	}
	if err := writeMessage(&buf, msg2); err != nil {
		t.Fatalf("write message 2: %v", err)
	}

	reader := bufio.NewReader(bytes.NewReader(buf.Bytes()))
	got1, err := readMessage(reader)
	if err != nil {
		t.Fatalf("read message 1: %v", err)
	}
	got2, err := readMessage(reader)
	if err != nil {
		t.Fatalf("read message 2: %v", err)
	}

	if string(got1) != string(msg1) {
		t.Fatalf("unexpected message 1: %s", string(got1))
	}
	if string(got2) != string(msg2) {
		t.Fatalf("unexpected message 2: %s", string(got2))
	}
}
