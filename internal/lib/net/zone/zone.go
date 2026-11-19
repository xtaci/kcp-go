package zone

import (
	"runtime"
	"strconv"
)

var zoneCache = ipv6ZoneCache{
	toIndex: make(map[string]int),
	toName:  make(map[int]string),
}

// Name converts an interface index to the platform's IPv6 zone representation.
func Name(zone int) string {
	switch {
	case zone == 0:
		return ""
	case runtime.GOOS == "windows":
		return strconv.Itoa(zone)
	default:
		return zoneCache.name(zone)
	}
}

// Index converts an IPv6 zone name or numeric zone to an interface index.
func Index(zone string) int {
	switch {
	case zone == "":
		return 0
	default:
		return zoneCache.index(zone)
	}
}
