package cbor

import (
	"bytes"
	"testing"
)

func mustBytes[T flusher](t *testing.T, buf *bytes.Buffer, enc T) []byte {
	t.Helper()
	if err := enc.Flush(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

type flusher interface {
	Flush() error
}
