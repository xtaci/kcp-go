package kcp

import (
	"sync"

	gouuid "github.com/satori/go.uuid"
)

var SHARD_COUNT = 1024

// A "thread" safe map of type string:Anything.
// To avoid lock bottlenecks this map is dived to several (SHARD_COUNT) map shards.
type ConcurrentMap []*ConcurrentMapShared

// A "thread" safe string to anything map.
type ConcurrentMapShared struct {
	items        map[gouuid.UUID]interface{}
	sync.RWMutex // Read Write mutex, guards access to internal map.
}

// Creates a new concurrent map.
func NewConcurrentMap() ConcurrentMap {
	m := make(ConcurrentMap, SHARD_COUNT)
	for i := 0; i < SHARD_COUNT; i++ {
		m[i] = &ConcurrentMapShared{items: make(map[gouuid.UUID]interface{})}
	}
	return m
}

// GetShard returns shard under given key
func (m ConcurrentMap) GetShard(key gouuid.UUID) *ConcurrentMapShared {
	return m[uint(fnv32(key))%uint(SHARD_COUNT)]
}

// Sets the given value under the specified key.
func (m ConcurrentMap) Set(key gouuid.UUID, value interface{}) {
	// Get map shard.
	shard := m.GetShard(key)
	shard.Lock()
	shard.items[key] = value
	shard.Unlock()
}

// Callback to return new element to be inserted into the map
// It is called while lock is held, therefore it MUST NOT
// try to access other keys in same map, as it can lead to deadlock since
// Go sync.RWLock is not reentrant
type ValueCb func() (interface{}, bool)

// Sets the given value under the specified key if no value was associated with it.
func (m ConcurrentMap) SetIfAbsent(key gouuid.UUID, cb ValueCb) {
	// Get map shard.
	shard := m.GetShard(key)
	shard.Lock()
	value, ok := shard.items[key]
	if !ok {
		value, ok = cb()
		if ok {
			shard.items[key] = value
		}
	}
	shard.Unlock()
}

// Get retrieves an element from map under given key.
func (m ConcurrentMap) Get(key gouuid.UUID) (interface{}, bool) {
	// Get shard
	shard := m.GetShard(key)
	shard.RLock()
	// Get item from shard.
	val, ok := shard.items[key]
	shard.RUnlock()
	return val, ok
}

// Remove removes an element from the map.
func (m ConcurrentMap) Remove(key gouuid.UUID) {
	// Try to get shard.
	shard := m.GetShard(key)
	shard.Lock()
	delete(shard.items, key)
	shard.Unlock()
}

func fnv32(key gouuid.UUID) uint32 {
	hash := uint32(2166136261)
	const prime32 = uint32(16777619)
	for i := 0; i < len(key); i++ {
		hash *= prime32
		hash ^= uint32(key[i])
	}
	return hash
}

// Iterator callback,called for every key,value found in
// maps. RLock is held for all calls for a given shard
// therefore callback sess consistent view of a shard,
// but not across the shards
type IterCb func(key gouuid.UUID, v interface{})

// Callback based iterator, cheapest way to read
// all elements in a map.
func (m ConcurrentMap) IterCb(fn IterCb) {
	for idx := range m {
		shard := (m)[idx]
		shard.RLock()
		for key, value := range shard.items {
			fn(key, value)
		}
		shard.RUnlock()
	}
}
