// The MIT License (MIT)
//
// Copyright (c) 2015 xtaci
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

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

	// minimal test
	tune = autoTune{}
	for i := 0; i < 1024; i++ {
		if i%maxAutoTuneSamples == 0 {
			tune.Sample(false, uint32(i))
		} else {
			tune.Sample(true, uint32(i))
		}
	}
	assert.NotEqual(t, 0, tune.pulses[0].seq)
	minSeq := tune.pulses[0].seq
	t.Log("minimal seq", tune.pulses[0].seq)

	tune.Sample(false, minSeq-1)
	assert.Equal(t, minSeq, tune.pulses[0].seq)
}
