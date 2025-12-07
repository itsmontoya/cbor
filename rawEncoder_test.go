package cbor

import (
	"bytes"
	"testing"
)

func TestRawEncoder_Primitives(t *testing.T) {
	var b bytes.Buffer
	enc := NewRawEncoder(&b, DefaultEncOptions)

	// 0..23 are single byte: 0x00..0x17
	if err := enc.Uint(10); err != nil {
		t.Fatal(err)
	}
	got := mustBytes(t, &b, enc)
	want := []byte{0x0a}
	if !bytes.Equal(got, want) {
		t.Fatalf("uint(10): got %x want %x", got, want)
	}

	b.Reset()
	enc = NewRawEncoder(&b, DefaultEncOptions)
	// -1 is 0x20, -2 is 0x21, so for i=-1 => nint(0) => 0x20
	if err := enc.Int(-1); err != nil {
		t.Fatal(err)
	}
	got = mustBytes(t, &b, enc)
	want = []byte{0x20}
	if !bytes.Equal(got, want) {
		t.Fatalf("int(-1): got %x want %x", got, want)
	}

	b.Reset()
	enc = NewRawEncoder(&b, DefaultEncOptions)
	if err := enc.Bool(true); err != nil {
		t.Fatal(err)
	}
	got = mustBytes(t, &b, enc)
	if !bytes.Equal(got, []byte{0xf5}) {
		t.Fatalf("true: got %x want f5", got)
	}

	b.Reset()
	enc = NewRawEncoder(&b, DefaultEncOptions)
	if err := enc.Null(); err != nil {
		t.Fatal(err)
	}
	got = mustBytes(t, &b, enc)
	if !bytes.Equal(got, []byte{0xf6}) {
		t.Fatalf("null: got %x want f6", got)
	}
}

func TestRawEncoder_TextAndBytes(t *testing.T) {
	var b bytes.Buffer
	enc := NewRawEncoder(&b, DefaultEncOptions)

	if err := enc.String("hi"); err != nil {
		t.Fatal(err)
	}
	got := mustBytes(t, &b, enc)
	// tstr, len 2 => 0x62, then 'h','i'
	want := []byte{0x62, 'h', 'i'}
	if !bytes.Equal(got, want) {
		t.Fatalf("string 'hi': got %x want %x", got, want)
	}

	b.Reset()
	enc = NewRawEncoder(&b, DefaultEncOptions)
	if err := enc.Bytes([]byte{1, 2, 3}); err != nil {
		t.Fatal(err)
	}
	got = mustBytes(t, &b, enc)
	// bstr, len 3 => 0x43, then 01 02 03
	want = []byte{0x43, 0x01, 0x02, 0x03}
	if !bytes.Equal(got, want) {
		t.Fatalf("bytes 010203: got %x want %x", got, want)
	}
}

func TestRawEncoder_BytesStream(t *testing.T) {
	var b bytes.Buffer
	enc := NewRawEncoder(&b, DefaultEncOptions)

	if err := enc.BytesStreamStart(); err != nil {
		t.Fatal(err)
	}
	if err := enc.BytesStreamChunk([]byte{1}); err != nil {
		t.Fatal(err)
	} // 0x41 01
	if err := enc.BytesStreamChunk([]byte{2, 3}); err != nil {
		t.Fatal(err)
	} // 0x42 02 03
	if err := enc.BytesStreamEnd(); err != nil {
		t.Fatal(err)
	}

	got := mustBytes(t, &b, enc)
	// 0x5f = indefinite bstr, chunks, 0xff break
	want := []byte{0x5f, 0x41, 0x01, 0x42, 0x02, 0x03, 0xff}
	if !bytes.Equal(got, want) {
		t.Fatalf("indef bstr: got %x want %x", got, want)
	}
}

func TestRawEncoder_ArrayMapDefinite(t *testing.T) {
	var b bytes.Buffer
	enc := NewRawEncoder(&b, DefaultEncOptions)

	// [1, "a"]
	if err := enc.ArrayStart(2); err != nil {
		t.Fatal(err)
	}
	if err := enc.Uint(1); err != nil {
		t.Fatal(err)
	}
	if err := enc.String("a"); err != nil {
		t.Fatal(err)
	}
	// definite array: no ArrayEnd
	got := mustBytes(t, &b, enc)
	want := []byte{0x82, 0x01, 0x61, 'a'}
	if !bytes.Equal(got, want) {
		t.Fatalf("array: got %x want %x", got, want)
	}

	// {"a": 1}
	b.Reset()
	enc = NewRawEncoder(&b, DefaultEncOptions)
	if err := enc.MapStart(1); err != nil {
		t.Fatal(err)
	}
	if err := enc.String("a"); err != nil {
		t.Fatal(err)
	}
	if err := enc.Uint(1); err != nil {
		t.Fatal(err)
	}
	got = mustBytes(t, &b, enc)
	want = []byte{0xa1, 0x61, 'a', 0x01}
	if !bytes.Equal(got, want) {
		t.Fatalf("map: got %x want %x", got, want)
	}
}

func TestRawEncoder_EncodeSortedMapPairs(t *testing.T) {
	var (
		k1, v1 bytes.Buffer
		k2, v2 bytes.Buffer
	)
	encK1 := NewRawEncoder(&k1, DefaultEncOptions)
	encV1 := NewRawEncoder(&v1, DefaultEncOptions)
	_ = encK1.String("a")
	_ = encK1.Flush()
	_ = encV1.Uint(1)
	_ = encV1.Flush()

	encK2 := NewRawEncoder(&k2, DefaultEncOptions)
	encV2 := NewRawEncoder(&v2, DefaultEncOptions)
	_ = encK2.String("b")
	_ = encK2.Flush()
	_ = encV2.Uint(2)
	_ = encV2.Flush()

	var out bytes.Buffer
	enc := NewRawEncoder(&out, DefaultEncOptions)
	if err := enc.EncodeSortedMapPairs([]Pair{
		{Key: k2.Bytes(), Value: v2.Bytes()},
		{Key: k1.Bytes(), Value: v1.Bytes()},
	}); err != nil {
		t.Fatal(err)
	}
	got := mustBytes(t, &out, enc)
	// { "a":1, "b":2 } definite map: 0xa2, 0x61 'a' 0x01, 0x61 'b' 0x02
	want := []byte{0xa2, 0x61, 'a', 0x01, 0x61, 'b', 0x02}
	if !bytes.Equal(got, want) {
		t.Fatalf("sorted map: got %x want %x", got, want)
	}
}
