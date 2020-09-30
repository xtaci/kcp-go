package kcp

import (
	"math/rand"
	"strconv"
	"sync"
	"testing"
)

func BenchmarkConcurrentMap_Write(b *testing.B) {
	dataLen := b.N
	concurrency := 8
	workLoad := dataLen / concurrency
	newMap := NewConcurrentMap()
	wait := &sync.WaitGroup{}

	b.ResetTimer()
	for i := 0; i < concurrency; i++ {
		wait.Add(1)
		go func(m *ConcurrentMap, start, end int, wait *sync.WaitGroup) {
			for i := start; i < end; i++ {
				m.Put(strconv.Itoa(i), i)
			}
			wait.Done()
		}(newMap, i*workLoad, (i+1)*workLoad, wait)
	}
	wait.Wait()
}

func BenchmarkSyncMap_Write(b *testing.B) {
	oldMap := &sync.Map{}
	dataLen := b.N
	concurrency := 8
	workLoad := dataLen / concurrency
	wait := &sync.WaitGroup{}
	b.ResetTimer()
	for i := 0; i < concurrency; i++ {
		wait.Add(1)
		go func(m *sync.Map, start, end int, wait *sync.WaitGroup) {
			for i := start; i < end; i++ {
				m.Store(strconv.Itoa(i), i)
			}
			wait.Done()
		}(oldMap, i*workLoad, (i+1)*workLoad, wait)
	}
	wait.Wait()
}

func BenchmarkConcurrentMap_ReadWrite(b *testing.B) {
	m := NewConcurrentMap()
	dataLen := 65536
	for i := 0; i < dataLen; i++ {
		m.Put(strconv.Itoa(i), i)
	}
	concurrency := 8
	wait := &sync.WaitGroup{}

	b.ResetTimer()
	for i := 0; i < concurrency; i++ {
		wait.Add(1)
		go func(w *sync.WaitGroup, m *ConcurrentMap) {
			for j := 0; j < b.N; j++ {
				l := rand.Intn(dataLen)
				if l%2 == 0 {
					m.Get(strconv.Itoa(l))
				} else {
					m.Put(strconv.Itoa(l), l+1)
				}
			}
			w.Done()
		}(wait, m)
	}
	wait.Wait()
}

func BenchmarkSyncMap_ReadWrite(b *testing.B) {
	m := &sync.Map{}
	dataLen := 65536
	for i := 0; i < dataLen; i++ {
		m.Store(strconv.Itoa(i), i)
	}
	concurrency := 8
	wait := &sync.WaitGroup{}

	b.ResetTimer()
	for i := 0; i < concurrency; i++ {
		wait.Add(1)
		go func(w *sync.WaitGroup, m *sync.Map) {
			for j := 0; j < b.N; j++ {
				l := rand.Intn(dataLen)
				if l%2 == 0 {
					m.Load(strconv.Itoa(l))
				} else {
					m.Store(strconv.Itoa(l), l+1)
				}
			}
			w.Done()
		}(wait, m)
	}
	wait.Wait()
}

func BenchmarkMapMutex_ReadWrite(b *testing.B) {
	m := make(map[string]int)
	lock := sync.Mutex{}
	dataLen := 65536
	for i := 0; i < dataLen; i++ {
		m[strconv.Itoa(i)] = i
	}
	concurrency := 8
	data := make([]string, dataLen)
	for i := 0; i < dataLen; i++ {
		data[i] = strconv.Itoa(i)
	}
	wait := &sync.WaitGroup{}

	b.ResetTimer()
	for i := 0; i < concurrency; i++ {
		wait.Add(1)
		go func(w *sync.WaitGroup, m map[string]int, ) {
			for j := 0; j < b.N; j++ {
				l := rand.Intn(dataLen)
				lock.Lock()
				if l%2 == 0 {
					_ = m[strconv.Itoa(l)]
				} else {
					m[strconv.Itoa(l)] = l+1
				}
				lock.Unlock()
			}
			w.Done()
		}(wait, m)
	}
	wait.Wait()
}

func TestConcurrentMap_DelAndLength(t *testing.T) {
	m := NewConcurrentMap()
	w := &sync.WaitGroup{}
	l := 1000
	c := 8
	for i := 0; i < c; i++ {
		w.Add(1)
		go func(start int, wait *sync.WaitGroup) {
			for j := start * l; j < (start+1)*l; j++ {
				m.Put(strconv.Itoa(j), j)
			}
			wait.Done()
		}(i, w)
	}
	w.Wait()

	if m.Length() != l*c {
		t.Errorf("Length not expected. Length returns %d, expected %d", m.Length(), l*c)
	}

	for i := 0; i < c; i++ {
		w.Add(1)
		go func(start int, wait *sync.WaitGroup) {
			for j := start * l; j < (start+1)*l; j++ {
				m.Del(strconv.Itoa(j))
			}
			wait.Done()
		}(i, w)
	}
	w.Wait()
	if m.Length() != 0 {
		t.Errorf("Length not expected. Length returns %d, expected  %d", m.Length(), 0)
	}
}

func TestConcurrentMap_PutAndGet(t *testing.T) {
	m := NewConcurrentMap()
	w := &sync.WaitGroup{}
	l := 10000
	c := 8
	workLoad := l / c

	for i := 0; i < c; i++ {
		w.Add(1)
		go func(w *sync.WaitGroup, start, end int) {
			for j := start; j < end; j++ {
				m.Put(strconv.Itoa(j), j)
			}
			w.Done()
		}(w, i*workLoad, (i+1)*workLoad)
	}
	w.Wait()

	for i := 0; i < c; i++ {
		w.Add(1)
		go func(w *sync.WaitGroup, start, end int) {
			for j := start; j < end; j++ {
				tmp, ok := m.Get(strconv.Itoa(j))
				if !ok || tmp.(int) != j {
					t.Errorf("Get not expexted. Get return: %v, expected:%d", tmp, j)
				}
			}
			w.Done()
		}(w, i*workLoad, (i+1)*workLoad)
	}
	w.Wait()
}

func TestConcurrentMap_Iterate(t *testing.T) {
	m := NewConcurrentMap()
	l := 10000
	type Value struct {
		Num int
	}
	for i := 0; i < l; i++ {
		m.Put(strconv.Itoa(i), &Value{Num: i})
	}
	m.Iterate(func(key string, val interface{}) bool {
		val.(*Value).Num++
		return true
	})
	for i := 0; i < l; i++ {
		tmp, _ := m.Get(strconv.Itoa(i))
		if tmp.(*Value).Num != i+1 {
			t.Errorf("Iterate not expected.")
			break
		}
	}
}

func TestConcurrentMap_Filter(t *testing.T) {
	m := NewConcurrentMap()
	l := 10000
	type Value struct {
		Num int
	}
	for i := 0; i < l; i++ {
		m.Put(strconv.Itoa(i), &Value{Num: i})
	}
	m.Filter(func(key string, val interface{}) bool {
		return val.(*Value).Num%2 == 0
	})
	for i := 0; i < l; i++ {
		_, ok := m.Get(strconv.Itoa(i))
		isEven := i%2 == 0
		if (isEven && !ok) || (!isEven && ok) {
			t.Errorf("Filter not expected")

		}
	}
}
