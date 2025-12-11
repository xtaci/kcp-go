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
	"container/heap"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAutoTune(t *testing.T) {
	// Group1
	signals := []uint32{0, 0, 0, 0, 0, 0}
	testGroup(t, 1, signals, -1, -1)

	// Group2
	signals = []uint32{0, 1, 0, 1, 0, 1}
	testGroup(t, 2, signals, 1, 1)

	// Group3
	signals = []uint32{1, 0, 1, 0, 0, 1}
	testGroup(t, 3, signals, 1, 1)

	// Group4
	signals = []uint32{1, 0, 0, 0, 0, 1}
	testGroup(t, 4, signals, 4, -1)

	// Group5
	signals = []uint32{1, 1, 1, 1, 1, 1}
	testGroup(t, 5, signals, -1, -1)

	// Group6
	signals = []uint32{1, 1, 0, 1, 1, 0}
	testGroup(t, 6, signals, 1, 2)

	// Group7
	signals = []uint32{0, 1, 1, 1, 0, 1}
	testGroup(t, 7, signals, 1, 3)

	// Group8
	signals = []uint32{1, 1, 1, 1, 1, 1}
	testGroup(t, 8, signals, -1, -1)

	// Group9
	signals = []uint32{0, 1, 1, 1, 1, 0}
	testGroup(t, 9, signals, -1, 4)

	// Group10
	signals = []uint32{0, 0, 1, 1, 0, 0}
	testGroup(t, 10, signals, -1, 2)

	// Group11
	signals = []uint32{0, 0, 0, 1, 1, 1}
	testGroup(t, 11, signals, -1, -1)
}

func testGroup(t *testing.T, gid int, signals []uint32, expectedFalse, expectedTrue int) {
	tune := autoTune{}
	for i, signal := range signals {
		if signal == 0 {
			tune.Sample(false, uint32(i))
		} else {
			tune.Sample(true, uint32(i))
		}
	}

	t.Log("Group#", gid, signals, tune.FindPeriod(false), tune.FindPeriod(true))
	assert.Equal(t, tune.FindPeriod(true), expectedTrue)
	assert.Equal(t, tune.FindPeriod(false), expectedFalse)
}

func TestAutoTuneOverflow(t *testing.T) {
	// minimal test
	tune := autoTune{}
	for i := range 1024 {
		if i%maxAutoTuneSamples == 0 {
			tune.Sample(false, uint32(i))
		} else {
			tune.Sample(true, uint32(i))
		}
		assert.LessOrEqual(t, len(tune.pulses), maxAutoTuneSamples)
	}

	assert.Equal(t, len(tune.pulses), maxAutoTuneSamples)
}

func TestAutoTunePop(t *testing.T) {
	signals := []uint32{0, 1, 1, 0, 0, 1, 0, 1, 1, 0, 0, 1, 0, 1, 1, 0}
	tune := autoTune{}
	for i, signal := range signals {
		if signal == 0 {
			tune.Sample(false, uint32(i))
		} else {
			tune.Sample(true, uint32(i))
		}
	}
	assert.Equal(t, tune.FindPeriod(false), 2)
	assert.Equal(t, tune.FindPeriod(true), 2)

	heap.Pop(&tune.pulses)

	assert.Equal(t, tune.FindPeriod(false), 2)
	assert.Equal(t, tune.FindPeriod(true), 1)

	// after popping more
	heap.Pop(&tune.pulses)
	heap.Pop(&tune.pulses)
	heap.Pop(&tune.pulses)

	assert.Equal(t, tune.FindPeriod(false), 1)
	assert.Equal(t, tune.FindPeriod(true), 1)
}
