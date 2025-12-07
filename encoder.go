package cbor

import (
	"bytes"
	"io"
	"reflect"
)

// NewEncoder wraps w with a buffered writer.
func NewEncoder(w io.Writer, opt EncoderOpts) *Encoder {
	var e Encoder
	e.r = NewRawEncoder(w, opt)
	return &e
}

type Encoder struct {
	r *RawEncoder
}

// Encode is the single public API for value encoding using reflection.
// It builds a type-specific encoder on first use (caching hook included below).
func (e *Encoder) Encode(v any) (err error) {
	m, ok := v.(Marshaler)
	if ok {
		return m.MarshalCBOR(e.r)
	}

	if v == nil {
		return e.r.Null()
	}

	rv := reflect.ValueOf(v)
	var fn encoderFn
	if fn, err = e.buildEncoder(rv.Type()); err != nil {
		return
	}

	return fn(e, rv)
}

func (e *Encoder) buildEncoder(t reflect.Type) (fn encoderFn, err error) {
	var ok bool
	if fn, ok = tc.Get(t); ok {
		return
	}

	switch t.Kind() {
	case reflect.Bool:
		fn = encodeBool
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		fn = encodeInt
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		fn = encodeUint
	case reflect.Float32:
		fn = encodeFloat32
	case reflect.Float64:
		fn = encodeFloat64
	case reflect.String:
		fn = encodeString

	case reflect.Slice:
		fn, err = e.generateSliceEncoder(t)
	case reflect.Array:
		fn, err = e.generateArrayEncoder(t)
	case reflect.Map:
		fn, err = e.generateMapEncoder(t, e.r.opt.Canonical)
	case reflect.Struct:
		fn, err = e.generateStructEncoder(t)

	case reflect.Interface:
		fn, err = e.generateInterfaceEncoder()
	case reflect.Pointer:
		fn, err = e.generatePointerEncoder(t)

	default:
		// Fallback: unsupported kinds (chan, func, unsafe, etc.)
		return nil, makeErrorUnsupportedType(t)
	}

	if err != nil {
		return nil, err
	}

	tc.Set(t, fn)
	return fn, nil
}

func (e *Encoder) generateSliceEncoder(t reflect.Type) (fn encoderFn, err error) {
	if t.Elem().Kind() == reflect.Uint8 {
		return encodeByteslice, nil
	}

	return e.generateArrayEncoder(t)
}

func (e *Encoder) generateArrayEncoder(t reflect.Type) (fn encoderFn, err error) {
	var elemEnc encoderFn
	elem := t.Elem()
	if elemEnc, err = e.buildEncoder(elem); err != nil {
		return
	}

	return func(e *Encoder, v reflect.Value) (err error) {
		if !v.IsValid() {
			return e.r.Null()
		}

		n := v.Len()
		if err = e.r.ArrayStart(n); err != nil {
			return err
		}

		for i := range n {
			if err = elemEnc(e, v.Index(i)); err != nil {
				return err
			}
		}

		// definite array => no ArrayEnd
		return nil
	}, nil
}

func (e *Encoder) generateMapEncoder(t reflect.Type, sorted bool) (fn encoderFn, err error) {
	var keyEnc, valEnc encoderFn
	if keyEnc, err = e.buildEncoder(t.Key()); err != nil {
		return
	}

	if valEnc, err = e.buildEncoder(t.Elem()); err != nil {
		return
	}

	if sorted {
		return e.generateSortedMapEncoder(keyEnc, valEnc)
	}

	return e.generateUnsortedMapEncoder(keyEnc, valEnc)
}

func (e *Encoder) generateUnsortedMapEncoder(keyEnc, valEnc encoderFn) (fn encoderFn, err error) {
	return func(e *Encoder, v reflect.Value) error {
		if !v.IsValid() || v.IsNil() {
			// Encode as empty map
			return e.r.MapStart(0)
		}

		keys := v.MapKeys()
		if err := e.r.MapStart(len(keys)); err != nil {
			return err
		}

		for _, k := range keys {
			if err := keyEnc(e, k); err != nil {
				return err
			}
			if err := valEnc(e, v.MapIndex(k)); err != nil {
				return err
			}
		}

		return nil
	}, nil
}

func (e *Encoder) generateSortedMapEncoder(keyEnc, valEnc encoderFn) (fn encoderFn, err error) {
	return func(e *Encoder, v reflect.Value) error {
		if !v.IsValid() || v.IsNil() {
			// Encode as empty map
			return e.r.MapStart(0)
		}

		keys := v.MapKeys()
		// pre-encode keys to bytes and sort canonically
		pairs := make([]Pair, 0, len(keys))
		for _, k := range keys {
			var kb, vb bytes.Buffer
			ke := NewEncoder(&kb, e.r.opt)
			ve := NewEncoder(&vb, e.r.opt)
			if err := keyEnc(ke, k); err != nil {
				return err
			}

			if err := ke.r.Flush(); err != nil {
				return err
			}

			if err := valEnc(ve, v.MapIndex(k)); err != nil {
				return err
			}

			if err := ve.r.Flush(); err != nil {
				return err
			}

			pairs = append(pairs, makePair(kb.Bytes(), vb.Bytes()))
		}

		return e.r.EncodeSortedMapPairs(pairs)
	}, nil
}

func (e *Encoder) generateStructEncoder(t reflect.Type) (fn encoderFn, err error) {
	var fields []structField
	if fields, err = e.makeStructFields(t); err != nil {
		return
	}

	return func(e *Encoder, v reflect.Value) error {
		if err := e.r.MapStart(-1); err != nil {
			return err
		}

		for _, f := range fields {
			fv := v.Field(f.index)
			if f.omitempty && isZero(fv) {
				continue
			}

			if err := e.r.String(f.name); err != nil {
				return err
			}

			if err := f.enc(e, fv); err != nil {
				return err
			}
		}

		return e.r.MapEnd()
	}, nil
}

func (e *Encoder) makeStructFields(t reflect.Type) (out []structField, err error) {
	n := t.NumField()
	out = make([]structField, 0, n)
	for i := range n {
		f := t.Field(i)
		if f.PkgPath != "" {
			// Pass unexported
			continue
		}

		tag := f.Tag.Get("cbor")
		if tag == "-" {
			continue
		}

		var sf structField
		if sf, err = e.makeStructField(i, tag, f); err != nil {
			return nil, err
		}

		out = append(out, sf)
	}

	return out, nil
}

func (e *Encoder) makeStructField(i int, tag string, sf reflect.StructField) (out structField, err error) {
	name, omitempty := parseTag(tag, sf.Name)
	out.index = i
	out.name = name
	out.omitempty = omitempty
	out.enc, err = e.buildEncoder(sf.Type)
	return
}

func (e *Encoder) generatePointerEncoder(t reflect.Type) (fn encoderFn, err error) {
	elem := t.Elem()

	var elemEnc encoderFn
	if elemEnc, err = e.buildEncoder(elem); err != nil {
		return
	}

	return func(e *Encoder, v reflect.Value) error {
		if v.IsNil() {
			return e.r.Null()
		}
		return elemEnc(e, v.Elem())
	}, nil
}

func (e *Encoder) generateInterfaceEncoder() (fn encoderFn, err error) {
	// Encode the dynamic concrete value or null if nil
	return func(e *Encoder, v reflect.Value) error {
		if v.IsNil() {
			return e.r.Null()
		}
		return e.Encode(v.Elem().Interface())
	}, nil
}

func encodeByteslice(e *Encoder, v reflect.Value) (err error) {
	if v.IsNil() {
		// CBOR uses null explicitly; []byte(nil) often better as empty bstr.
		// Choose empty bstr to match common Go semantics:
		return e.r.Bytes(nil)
	}

	return e.r.Bytes(v.Bytes())
}

func encodeBool(e *Encoder, v reflect.Value) error {
	return e.r.Bool(v.Bool())
}

func encodeInt(e *Encoder, v reflect.Value) error {
	return e.r.Int(v.Int())
}

func encodeUint(e *Encoder, v reflect.Value) error {
	return e.r.Uint(v.Uint())
}

func encodeFloat32(e *Encoder, v reflect.Value) error {
	return e.r.Float32(float32(v.Float()))
}

func encodeFloat64(e *Encoder, v reflect.Value) error {
	return e.r.Float64(v.Float())
}

func encodeString(e *Encoder, v reflect.Value) error {
	return e.r.String(v.String())
}
