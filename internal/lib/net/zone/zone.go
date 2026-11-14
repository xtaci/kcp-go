package zone

import (
	"runtime"
	"strconv"
)

var zoneCache = ipv6ZoneCache{
	toIndex: make(map[string]int),
	toName:  make(map[int]string),
}

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

func Index(zone string) int {
	switch {
	case zone == "":
		return 0
	default:
		return zoneCache.index(zone)
	}
}
