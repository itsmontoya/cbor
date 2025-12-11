package cbor

import (
	"bufio"
	"io"
	"reflect"
	"strings"
)

func getBufioWriter(w io.Writer) *bufio.Writer {
	if bw, ok := w.(*bufio.Writer); ok {
		return bw
	}

	return bufio.NewWriterSize(w, 16<<10)
}

func getBufioReader(r io.Reader) *bufio.Reader {
	if bw, ok := r.(*bufio.Reader); ok {
		return bw
	}

	return bufio.NewReaderSize(r, 16<<10)
}

func parseTag(tag, fallback string) (name string, omitempty bool) {
	if tag == "" {
		return fallback, false
	}

	parts := strings.Split(tag, ",")
	if parts[0] != "" {
		fallback = parts[0]
	}

	for _, p := range parts[1:] {
		switch strings.TrimSpace(p) {
		case "omitempty":
			omitempty = true
		}
	}

	return fallback, omitempty
}

func isZero(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Bool:
		return !v.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int() == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return v.Uint() == 0
	case reflect.Float32, reflect.Float64:
		return v.Float() == 0
	case reflect.String:
		return v.Len() == 0
	case reflect.Slice, reflect.Map:
		return v.Len() == 0 || v.IsNil()
	case reflect.Interface, reflect.Pointer:
		return v.IsNil()
	case reflect.Array:
		// Check each element (rarely used for omitempty on arrays)
		for i := 0; i < v.Len(); i++ {
			if !isZero(v.Index(i)) {
				return false
			}
		}
		return true
	case reflect.Struct:
		// Zero if all fields zero
		for i := 0; i < v.NumField(); i++ {
			if v.Field(i).CanInterface() && !isZero(v.Field(i)) {
				return false
			}
		}
		return true
	default:
		return false
	}
}
