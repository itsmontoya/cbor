package cbor

import (
	"reflect"
	"sync"
)

var tc typeCache = typeCache{m: make(map[reflect.Type]encoderFn)}

type typeCache struct {
	mux sync.RWMutex
	m   map[reflect.Type]encoderFn
}

func (c *typeCache) Get(t reflect.Type) (fn encoderFn, ok bool) {
	c.mux.RLock()
	defer c.mux.RUnlock()
	fn, ok = c.m[t]
	return
}

func (c *typeCache) Set(t reflect.Type, fn encoderFn) {
	c.mux.Lock()
	defer c.mux.Unlock()
	if _, ok := c.m[t]; ok {
		return
	}

	c.m[t] = fn
}
