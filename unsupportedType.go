package cbor

import "reflect"

func makeErrorUnsupportedType(t reflect.Type) error {
	return &ErrorUnsupportedType{t: t}
}

type ErrorUnsupportedType struct{ t reflect.Type }

func (e *ErrorUnsupportedType) Error() string {
	return "cbor: unsupported type: " + e.t.String()
}
