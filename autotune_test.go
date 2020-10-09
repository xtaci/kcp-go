package kcp

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAutoTune(t *testing.T) {
	signals := []uint32{0, 0, 0, 0, 0, 0}

	tune := autoTune{}
	for i := 0; i < len(signals); i++ {
		if signals[i] == 0 {
			tune.Sample(false, uint32(i))
		} else {
			tune.Sample(true, uint32(i))
		}
	}

	assert.Equal(t, -1, tune.FindPeriod(false))
	assert.Equal(t, -1, tune.FindPeriod(true))

	signals = []uint32{1, 0, 1, 0, 0, 1}
	tune = autoTune{}
	for i := 0; i < len(signals); i++ {
		if signals[i] == 0 {
			tune.Sample(false, uint32(i))
		} else {
			tune.Sample(true, uint32(i))
		}
	}
	assert.Equal(t, 1, tune.FindPeriod(false))
	assert.Equal(t, 1, tune.FindPeriod(true))

	signals = []uint32{1, 0, 0, 0, 0, 1}
	tune = autoTune{}
	for i := 0; i < len(signals); i++ {
		if signals[i] == 0 {
			tune.Sample(false, uint32(i))
		} else {
			tune.Sample(true, uint32(i))
		}
	}
	assert.Equal(t, -1, tune.FindPeriod(true))
	assert.Equal(t, 4, tune.FindPeriod(false))
}
