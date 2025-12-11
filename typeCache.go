package cbor

import (
	"reflect"
	"sync"
)

var (
	etc typeCache[encoderFn] = typeCache[encoderFn]{m: make(map[reflect.Type]encoderFn)}
	dtc typeCache[decoderFn] = typeCache[decoderFn]{m: make(map[reflect.Type]decoderFn)}
)

type typeCache[v any] struct {
	mux sync.RWMutex
	m   map[reflect.Type]v
}

func (c *typeCache[v]) Get(t reflect.Type) (fn v, ok bool) {
	c.mux.RLock()
	defer c.mux.RUnlock()
	fn, ok = c.m[t]
	return
}

func (c *typeCache[v]) Set(t reflect.Type, fn v) {
	c.mux.Lock()
	defer c.mux.Unlock()
	if _, ok := c.m[t]; ok {
		return
	}

	c.m[t] = fn
}
