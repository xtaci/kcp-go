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

const maxAutoTuneSamples = 512 // two 256-shard sets, aligned to bitmap words

// autoTune detects the repeating period of data and parity shards in the
// incoming FEC stream. This allows the decoder to auto-detect the peer's
// dataShards/parityShards configuration.
//
// Samples occupy a sliding window of up to maxAutoTuneSamples sequence
// numbers. Older samples outside the window are ignored, and duplicates do
// not advance it. Missing and conflicting samples break continuity.
//
// Each ring position uses two adjacent bits (present, signal): 00 means
// missing, 10 is parity, 11 is data, and 01 is a conflicting duplicate.
// A conflict remains unusable until that position leaves the window.
type autoTune struct {
	samples [maxAutoTuneSamples / 32]uint64
	base    uint32 // sequence number at head
	head    int    // ring index of base
	count   int    // sequence positions in the window, including gaps
}

// clear resets a ring position to the missing state before reuse.
func (tune *autoTune) clear(index int) {
	tune.samples[index/32] &^= uint64(3) << (2 * (index % 32))
}

// sampleAt returns the two-bit state of a ring position.
func (tune *autoTune) sampleAt(index int) uint64 {
	return (tune.samples[index/32] >> (2 * (index % 32))) & 3
}

// Sample adds a signal sample to the sequence-number window. Sequence
// comparisons use modular uint32 arithmetic and assume samples are less than
// 2^31 positions apart.
func (tune *autoTune) Sample(bit bool, seq uint32) {
	if tune.count == 0 {
		tune.base = seq
		tune.count = 1
	}

	offset := int64(_itimediff(seq, tune.base))
	if offset < 0 {
		// Accept reordered samples before the base only if the entire span fits.
		back := -offset
		if back+int64(tune.count) > maxAutoTuneSamples {
			return
		}
		for range int(back) {
			tune.head = (tune.head + maxAutoTuneSamples - 1) % maxAutoTuneSamples
			tune.clear(tune.head)
		}
		tune.base = seq
		tune.count += int(back)
		offset = 0
	} else if offset >= maxAutoTuneSamples {
		advance := offset - maxAutoTuneSamples + 1
		if advance >= int64(tune.count) {
			// The new window no longer overlaps any existing sample.
			tune.samples = [len(tune.samples)]uint64{}
			tune.head = 0
			tune.base = seq
			tune.count = 1
			offset = 0
		} else {
			// Clear evicted positions before their ring slots are reused.
			for range int(advance) {
				tune.clear(tune.head)
				tune.head = (tune.head + 1) % maxAutoTuneSamples
			}
			tune.base += uint32(advance)
			tune.count -= int(advance)
			offset -= advance
		}
	}

	if int(offset) >= tune.count {
		tune.count = int(offset) + 1
	}
	index := (tune.head + int(offset)) % maxAutoTuneSamples
	shift := 2 * (index % 32)
	word := index / 32
	value := uint64(2)
	if bit {
		value |= 1
	}
	state := tune.sampleAt(index)
	if state == 0 {
		tune.samples[word] |= value << shift
	} else if state&2 != 0 && state != value {
		// Conflicts remain distinct from missing slots on later arrivals.
		tune.samples[word] &^= uint64(2) << shift
		tune.samples[word] |= uint64(1) << shift
	}
}

// FindPeriod detects the period (pulse width) of the given signal type
// in the collected samples. Returns -1 if no valid period is found.
//
// For bit=true (data shards), it finds how many consecutive data shards
// appear before a parity shard, giving us the dataShards count.
// For bit=false (parity shards), it gives the parityShards count.
//
// The detection works by finding a complete pulse in sequence order:
//  1. Find the "left edge": where the signal transitions TO the target bit
//  2. Find the "right edge": where the signal transitions AWAY from the target bit
//  3. The period = rightEdge - leftEdge
//
// Example signal (bit=true looking for data period):
//
//	Signal Level
//	    |
//	1.0 |                 _____           _____
//	    |                |     |         |     |
//	0.5 |      _____     |     |   _____ |     |   _____
//	    |     |     |    |     |  |     ||     |  |     |
//	0.0 |_____|     |____|     |__|     ||     |__|     |_____
//	    |
//	    |-----------------------------------------------------> Time
//	         A     B    C     D  E     F     G  H     I
func (tune *autoTune) FindPeriod(bit bool) int {
	// At least three sequence positions are needed for both edges.
	if tune.count < 3 {
		return -1
	}

	left := -1
	previous := false
	havePrevious := false
	for i := 0; i < tune.count; i++ {
		index := (tune.head + i) % maxAutoTuneSamples
		state := tune.sampleAt(index)
		if state&2 == 0 {
			// A missing or conflicting sample can hide a transition.
			left = -1
			havePrevious = false
			continue
		}
		current := state&1 != 0
		if havePrevious && previous != bit && current == bit {
			left = i
		} else if left >= 0 && current != bit {
			return i - left
		}
		previous = current
		havePrevious = true
	}
	return -1
}
