package cbor

import "reflect"

// encoderFn encodes v's *value* (non-allocating where possible) to e.
type encoderFn func(e *Encoder, v reflect.Value) error
