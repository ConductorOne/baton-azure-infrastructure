package connector

import "sync"

type GenericCache[T any] struct {
	cache map[string]T
	mutex sync.Mutex
}

func NewGenericCache[T any]() *GenericCache[T] {
	return &GenericCache[T]{
		cache: make(map[string]T),
		mutex: sync.Mutex{},
	}
}

func (c *GenericCache[T]) Get(key string) (T, bool) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	value, ok := c.cache[key]
	return value, ok
}

func (c *GenericCache[T]) Set(key string, value T) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	c.cache[key] = value
}

func (c *GenericCache[T]) GetOrSet(key string, setValue func() (T, error)) (T, error) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	value, ok := c.cache[key]
	if ok {
		return value, nil
	}

	value, err := setValue()
	if err != nil {
		var defaultValue T
		return defaultValue, err
	}
	c.cache[key] = value
	return value, nil
}
