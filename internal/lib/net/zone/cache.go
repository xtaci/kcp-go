// Copyright 2017 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package zone

import (
	"net"
	"strconv"
	"sync"
	"time"
)

// An ipv6ZoneCache represents a cache holding partial network
// interface information. It is used for reducing the cost of IPv6
// addressing scope zone resolution.
//
// Multiple names sharing the index are managed by first-come
// first-served basis for consistency.
type ipv6ZoneCache struct {
	sync.RWMutex                // guard the following
	lastFetched  time.Time      // last time routing information was fetched
	toIndex      map[string]int // interface name to its index
	toName       map[int]string // interface index to its name
}

// update refreshes the network interface information if the cache was last
// updated more than 1 minute ago, or if force is set. It returns whether the
// cache was updated.
func (zc *ipv6ZoneCache) update(ift []net.Interface, force bool) (updated bool) {
	zc.Lock()
	defer zc.Unlock()
	now := time.Now()
	if !force && zc.lastFetched.After(now.Add(-60*time.Second)) {
		return false
	}
	zc.lastFetched = now
	if len(ift) == 0 {
		var err error
		if ift, err = net.Interfaces(); err != nil {
			return false
		}
	}
	zc.toIndex = make(map[string]int, len(ift))
	zc.toName = make(map[int]string, len(ift))
	for _, ifi := range ift {
		zc.toIndex[ifi.Name] = ifi.Index
		if _, ok := zc.toName[ifi.Index]; !ok {
			zc.toName[ifi.Index] = ifi.Name
		}
	}
	return true
}

func (zc *ipv6ZoneCache) name(zone int) string {
	updated := zc.update(nil, false)
	zc.RLock()
	name, ok := zc.toName[zone]
	zc.RUnlock()
	if !ok && !updated {
		zc.update(nil, true)
		zc.RLock()
		name, ok = zc.toName[zone]
		zc.RUnlock()
	}
	if !ok { // last resort
		name = strconv.Itoa(zone)
	}
	return name
}

func (zc *ipv6ZoneCache) index(zone string) int {
	updated := zc.update(nil, false)
	zc.RLock()
	index, ok := zc.toIndex[zone]
	zc.RUnlock()
	if !ok && !updated {
		zc.update(nil, true)
		zc.RLock()
		index, ok = zc.toIndex[zone]
		zc.RUnlock()
	}
	if !ok { // last resort
		index, _ = strconv.Atoi(zone)
	}
	return index
}
