package kcp

import (
	"github.com/OneOfOne/xxhash"
	"sync"
	"sync/atomic"
)

const (
	segmentCount = 256
	segmentMask  = 255
)

type ConcurrentMap struct {
	segments [segmentCount]map[string]interface{}
	locks    [segmentCount]sync.RWMutex
	length   int64
}

func (m *ConcurrentMap) Put(key string, val interface{}) {
	keyHash := xxhash.Checksum32([]byte(key))
	segIdx := keyHash & segmentMask
	m.locks[segIdx].Lock()
	defer m.locks[segIdx].Unlock()
	if seg := m.segments[segIdx]; seg != nil {
		if _, ok := seg[key]; !ok {
			atomic.AddInt64(&m.length, 1)
		}
		seg[key] = val
	} else {
		seg = make(map[string]interface{})
		seg[key] = val
		m.segments[segIdx] = seg
		atomic.AddInt64(&m.length, 1)
	}
}

func (m *ConcurrentMap) Get(key string) (interface{}, bool) {
	keyHash := xxhash.Checksum32([]byte(key))
	segIdx := keyHash & segmentMask
	m.locks[segIdx].RLock()
	defer m.locks[segIdx].RUnlock()
	if seg := m.segments[segIdx]; seg == nil {
		return nil, false
	} else {
		val, ok := seg[key]
		return val, ok
	}
}

func (m *ConcurrentMap) Del(key string) {
	keyHash := xxhash.Checksum32([]byte(key))
	segIdx := keyHash & segmentMask
	m.locks[segIdx].Lock()
	defer m.locks[segIdx].Unlock()
	if seg := m.segments[segIdx]; seg != nil {
		if _, ok := seg[key]; ok {
			atomic.AddInt64(&m.length, -1)
		}
		delete(seg, key)
	}
}

type IterFun func(key string, val interface{}) bool

func (m *ConcurrentMap) Iterate(f IterFun) {
	for i := 0; i < segmentCount; i++ {
		func(i int) {
			m.locks[i].RLock()
			defer m.locks[i].RUnlock()
			tmp := m.segments[i]
			for k, v := range tmp {
				if !f(k, v) {
					break
				}
			}
		}(i)
	}
}

func (m *ConcurrentMap) Length() int {
	return int(m.length)
}

func (m *ConcurrentMap) Filter(f IterFun) {
	for i := 0; i < segmentCount; i++ {
		func(i int) {
			m.locks[i].Lock()
			defer m.locks[i].Unlock()
			tmp := m.segments[i]
			var deleteKeys []string
			for k, v := range tmp {
				if !f(k, v) {
					deleteKeys = append(deleteKeys, k)
				}
			}
			for _, k := range deleteKeys {
				delete(tmp, k)
			}
		}(i)
	}
}

func NewConcurrentMap() *ConcurrentMap {
	return &ConcurrentMap{}
}
