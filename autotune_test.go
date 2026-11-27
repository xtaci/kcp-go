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
	"encoding/binary"
	"fmt"
	"math/rand"
	"slices"
	"strings"
	"testing"
	"time"
	"unsafe"

	"github.com/stretchr/testify/assert"
)

// Auto-tune coverage includes:
//   - ring-buffer overwrite, pulse-width changes, and sequence-number wraparound;
//   - exhaustive bounded signal, loss, gap, arrival-order, duplicate, and
//     conflicting-duplicate patterns;
//   - FEC decoder reconfiguration across small and boundary shard counts,
//     convergence after a lost shard, and rejection above the shard limit.
//
// The exhaustive matrices cover finite sample windows. They cannot enumerate
// an unbounded packet stream, so focused tests cover long-running state changes.

// pulse is the independent sequence-and-signal representation used by the
// test oracle for gap patterns.
type pulse struct {
	bit bool
	seq uint32
}

// logAutoTunePattern renders the sequence window for FEC test diagnostics.
func logAutoTunePattern(t *testing.T, tune *autoTune) {
	t.Helper()
	if tune.count == 0 {
		t.Log("pattern: <empty>")
		return
	}

	var pattern strings.Builder
	for i := range tune.count {
		index := (tune.head + i) % maxAutoTuneSamples
		mask := uint64(1) << (index % 64)
		switch {
		case tune.present[index/64]&mask == 0:
			pattern.WriteByte('.')
		case tune.bits[index/64]&mask != 0:
			pattern.WriteByte('#')
		default:
			pattern.WriteByte('_')
		}
	}
	t.Logf("pattern: %s (#=data/high, _=parity/low, .=missing or conflicting)", pattern.String())
}

// TestAutoTuneOverflow verifies that sustained input cannot grow the window.
func TestAutoTuneOverflow(t *testing.T) {
	tune := autoTune{}
	for i := range 1024 {
		if i%maxAutoTuneSamples == 0 {
			tune.Sample(false, uint32(i))
		} else {
			tune.Sample(true, uint32(i))
		}
		assert.LessOrEqual(t, tune.count, maxAutoTuneSamples)
	}

	assert.Equal(t, maxAutoTuneSamples, tune.count)
}

// TestAutoTuneSignalSuffixes verifies that detection starts at the first
// complete pulse, not at a partial pulse at the beginning of a window.
func TestAutoTuneSignalSuffixes(t *testing.T) {
	signals := []uint32{0, 1, 1, 0, 0, 1, 0, 1, 1, 0, 0, 1, 0, 1, 1, 0}
	for _, tt := range []struct {
		start        int
		data, parity int
	}{
		{0, 2, 2},
		{1, 1, 2},
		{4, 1, 1},
	} {
		tune := autoTune{}
		for i := tt.start; i < len(signals); i++ {
			tune.Sample(signals[i] != 0, uint32(i))
		}
		assert.Equal(t, tt.parity, tune.FindPeriod(false))
		assert.Equal(t, tt.data, tune.FindPeriod(true))
	}
}

// TestAutoTuneSequenceWindow exercises duplicate, stale and distant samples,
// including when the window crosses the uint32 sequence boundary.
func TestAutoTuneSequenceWindow(t *testing.T) {
	for _, start := range []uint32{0, ^uint32(0) - 100} {
		t.Run(fmt.Sprint(start), func(t *testing.T) {
			tune := autoTune{}
			for i := range maxAutoTuneSamples {
				tune.Sample(i%2 != 0, start+uint32(i))
			}
			// Duplicate and stale samples must not evict the current window.
			for range maxAutoTuneSamples + 1 {
				tune.Sample(true, start+1)
				tune.Sample(false, start-1)
			}
			assert.Equal(t, start, tune.base)
			assert.Equal(t, 1, tune.FindPeriod(true))

			// Advancing the window clears the reused slot, including conflicts.
			tune.Sample(true, start)
			tune.Sample(true, start+uint32(maxAutoTuneSamples))
			assert.Equal(t, start+1, tune.base)
			assert.Equal(t, 1, tune.FindPeriod(true))

			// A distant jump drops old samples; late arrivals can fill gaps.
			far := start + 10000
			tune.Sample(false, far)
			assert.Equal(t, -1, tune.FindPeriod(true))
			tune.Sample(false, far-2)
			tune.Sample(true, far-1)
			assert.Equal(t, 1, tune.FindPeriod(true))
		})
	}
}

// TestAutoTuneConflictStaysMissing ensures a later duplicate cannot restore
// a sequence position after conflicting shard types were observed.
func TestAutoTuneConflictStaysMissing(t *testing.T) {
	tune := autoTune{}
	tune.Sample(false, 0)
	tune.Sample(true, 1)
	tune.Sample(false, 1)
	tune.Sample(true, 1)
	tune.Sample(false, 2)
	assert.Equal(t, -1, tune.FindPeriod(true))
}

// TestAutoTuneEvictsConflict verifies that a reused ring slot loses its old
// conflict marker and accepts the new sequence's sample.
func TestAutoTuneEvictsConflict(t *testing.T) {
	tune := autoTune{}
	tune.Sample(false, 0)
	tune.Sample(true, 0)
	for seq := uint32(1); seq < maxAutoTuneSamples; seq++ {
		tune.Sample(false, seq)
	}
	tune.Sample(true, maxAutoTuneSamples)
	tune.Sample(false, maxAutoTuneSamples+1)
	assert.Equal(t, 1, tune.FindPeriod(true))
}

// TestAutoTuneCompactStorage guards the per-decoder memory reduction.
func TestAutoTuneCompactStorage(t *testing.T) {
	if size := unsafe.Sizeof(autoTune{}); size > 160 {
		t.Fatalf("autoTune uses %d bytes, want at most 160", size)
	}
}

// TestAutoTuneWindowCapacity checks retention and eviction at the window limit.
func TestAutoTuneWindowCapacity(t *testing.T) {
	tune := autoTune{}
	tune.Sample(false, 0)
	tune.Sample(true, 1)
	tune.Sample(false, 2)
	tune.Sample(false, 511)
	assert.Equal(t, 1, tune.FindPeriod(true))
	tune.Sample(false, 512)
	assert.Equal(t, -1, tune.FindPeriod(true))
}

// TestAutoTuneSlidingWindowAgainstReference compares each update with a
// sequence-keyed model across repeated window shifts and sequence wraparound.
func TestAutoTuneSlidingWindowAgainstReference(t *testing.T) {
	const (
		parity   = 1
		data     = 2
		conflict = 3
	)
	for _, start := range []uint32{0, ^uint32(0) - 128} {
		t.Run(fmt.Sprint(start), func(t *testing.T) {
			rng := rand.New(rand.NewSource(42))
			tune := autoTune{}
			samples := make(map[int]int)
			newest, previousSeq := 0, 0
			previousBit := false
			for step := range 5000 {
				seq := newest
				bit := rng.Intn(5) < 3
				switch action := rng.Intn(200); {
				case step == 0:
				case action < 110:
					seq += 1 + rng.Intn(4)
				case action < 160:
					seq -= rng.Intn(maxAutoTuneSamples + 30)
				case action < 199:
					seq, bit = previousSeq, previousBit
					if rng.Intn(4) == 0 {
						bit = !bit
					}
				default:
					seq += maxAutoTuneSamples + rng.Intn(300)
				}
				previousSeq, previousBit = seq, bit
				tune.Sample(bit, start+uint32(seq))

				if seq > newest {
					newest = seq
					for old := range samples {
						if old < newest-maxAutoTuneSamples+1 {
							delete(samples, old)
						}
					}
				}
				if seq >= newest-maxAutoTuneSamples+1 {
					value := parity
					if bit {
						value = data
					}
					if old := samples[seq]; old != 0 && old != value {
						value = conflict
					}
					samples[seq] = value
				}

				for _, target := range []int{parity, data} {
					want, left, previous := -1, 0, 0
					inside := false
					for pos := newest - maxAutoTuneSamples + 1; pos <= newest; pos++ {
						current := samples[pos]
						if current == 0 || current == conflict {
							inside, previous = false, 0
							continue
						}
						if previous != 0 && previous != target && current == target {
							left = pos
							inside = true
						} else if inside && current != target {
							want = pos - left
							break
						}
						previous = current
					}
					if got := tune.FindPeriod(target == data); got != want {
						t.Fatalf("step=%d seq=%d target=%d: got %d, want %d", step, seq, target, got, want)
					}
				}
			}
		})
	}
}

// TestAutoTuneRingBufferWrapAround tests that the ring buffer handles
// wrap-around and period detection returns a valid result after many overwrites.
func TestAutoTuneRingBufferWrapAround(t *testing.T) {
	// Test with various periods after buffer wrap-around
	testCases := []struct {
		name           string
		period         int
		totalSamples   int
		expectedPeriod int
		bit            bool
	}{
		{"Period3_2xBuffer", 3, maxAutoTuneSamples * 2, 1, true},
		{"Period5_3xBuffer", 5, maxAutoTuneSamples * 3, 2, true},
		{"Period7_4xBuffer", 7, maxAutoTuneSamples * 4, 3, true},
		{"Period10_2xBuffer", 10, maxAutoTuneSamples * 2, 5, true},
		{"Period3_2xBuffer_False", 3, maxAutoTuneSamples * 2, 2, false},
		{"Period5_3xBuffer_False", 5, maxAutoTuneSamples * 3, 3, false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tune := autoTune{}
			period := tc.period
			halfPeriod := period / 2
			if halfPeriod == 0 {
				halfPeriod = 1
			}

			// Generate periodic signal: N samples of 1, then N samples of 0
			for i := 0; i < tc.totalSamples; i++ {
				posInPeriod := i % period
				bit := posInPeriod < halfPeriod
				tune.Sample(bit, uint32(i))
			}

			// Verify buffer is at maximum capacity
			assert.Equal(t, maxAutoTuneSamples, tune.count)

			// Find period and verify
			foundPeriod := tune.FindPeriod(tc.bit)
			t.Logf("Period=%d, TotalSamples=%d, Found=%d, Expected=%d",
				tc.period, tc.totalSamples, foundPeriod, tc.expectedPeriod)

			assert.Equal(t, tc.expectedPeriod, foundPeriod)
		})
	}
}

// TestAutoTuneStablePeriodAfterOverwrite tests that the period remains stable
// even after the ring buffer has been overwritten multiple times.
func TestAutoTuneStablePeriodAfterOverwrite(t *testing.T) {
	tune := autoTune{}
	period := 6 // total period
	trueDuration := 3

	// Track periods found during continuous sampling
	var periodsFound []int

	// Generate a lot of samples with a consistent period
	totalSamples := maxAutoTuneSamples * 5
	for i := range totalSamples {
		posInPeriod := i % period
		bit := posInPeriod < trueDuration
		tune.Sample(bit, uint32(i))

		// After buffer is full, periodically check the found period
		if tune.count == maxAutoTuneSamples && i%50 == 0 {
			p := tune.FindPeriod(true)
			if p > 0 {
				periodsFound = append(periodsFound, p)
			}
		}
	}

	// All found periods should be consistent (same value)
	assert.NotEmpty(t, periodsFound, "Should have found periods during sampling")

	// Check that periods are stable (all the same or very close)
	if len(periodsFound) > 1 {
		firstPeriod := periodsFound[0]
		for i, p := range periodsFound {
			assert.Equal(t, firstPeriod, p,
				"Period at sample %d should match first period", i)
		}
		t.Logf("Found stable period %d across %d checks after buffer overwrite",
			firstPeriod, len(periodsFound))
	}
}

// TestAutoTuneVariousPeriodsFull tests various period lengths when buffer is full
func TestAutoTuneVariousPeriodsFull(t *testing.T) {
	// Test different period values
	periods := []int{2, 4, 6, 8, 10, 12, 16, 20, 32, 50}

	for _, period := range periods {
		t.Run("Period"+string(rune('0'+period/10))+string(rune('0'+period%10)), func(t *testing.T) {
			tune := autoTune{}
			trueDuration := period / 2
			if trueDuration == 0 {
				trueDuration = 1
			}

			// Fill buffer completely and then some more
			totalSamples := maxAutoTuneSamples + 100
			for i := range totalSamples {
				posInPeriod := i % period
				bit := posInPeriod < trueDuration
				tune.Sample(bit, uint32(i))
			}

			assert.Equal(t, maxAutoTuneSamples, tune.count)

			periodTrue := tune.FindPeriod(true)
			periodFalse := tune.FindPeriod(false)

			t.Logf("Period=%d, TrueDuration=%d, FoundTrue=%d, FoundFalse=%d",
				period, trueDuration, periodTrue, periodFalse)

			assert.Equal(t, trueDuration, periodTrue)
			assert.Equal(t, period-trueDuration, periodFalse)
		})
	}
}

// TestAutoTuneContinuousOverwrite tests continuous overwrite scenarios
// to ensure the ring buffer correctly maintains period detection capability
func TestAutoTuneContinuousOverwrite(t *testing.T) {
	tune := autoTune{}

	// First phase: fill with period 4 signal
	period1 := 4
	for i := range maxAutoTuneSamples {
		bit := (i % period1) < (period1 / 2)
		tune.Sample(bit, uint32(i))
	}
	assert.Equal(t, maxAutoTuneSamples, tune.count)

	p1 := tune.FindPeriod(true)
	t.Logf("Phase 1: Period 4 signal, found period=%d", p1)
	assert.Equal(t, 2, p1, "Should find the data-shard width in phase 1")

	// Second phase: continue with period 6 signal
	// After enough samples, the old period 4 data should be overwritten
	period2 := 6
	baseSeq := uint32(maxAutoTuneSamples)
	for i := range maxAutoTuneSamples {
		bit := (i % period2) < (period2 / 2)
		tune.Sample(bit, baseSeq+uint32(i))
	}

	p2 := tune.FindPeriod(true)
	t.Logf("Phase 2: Period 6 signal, found period=%d", p2)
	assert.Equal(t, 3, p2, "Should find the data-shard width in phase 2")

	// Third phase: continue with period 8 signal
	period3 := 8
	baseSeq = uint32(maxAutoTuneSamples * 2)
	for i := range maxAutoTuneSamples {
		bit := (i % period3) < (period3 / 2)
		tune.Sample(bit, baseSeq+uint32(i))
	}

	p3 := tune.FindPeriod(true)
	t.Logf("Phase 3: Period 8 signal, found period=%d", p3)
	assert.Equal(t, 4, p3, "Should find the data-shard width in phase 3")
}

// TestAutoTunePeriodChangeAfterOverwrite tests that when signal pattern changes,
// the new period can be correctly detected after old data is fully overwritten
func TestAutoTunePeriodChangeAfterOverwrite(t *testing.T) {
	testCases := []struct {
		name             string
		oldTrueDuration  int
		oldFalseDuration int
		newTrueDuration  int
		newFalseDuration int
		expectedNewTrue  int
		expectedNewFalse int
	}{
		{"Period_2to4", 1, 1, 2, 2, 2, 2},
		{"Period_4to2", 2, 2, 1, 1, 1, 1},
		{"Period_4to8", 2, 2, 4, 4, 4, 4},
		{"Period_8to4", 4, 4, 2, 2, 2, 2},
		{"Period_6to10", 3, 3, 5, 5, 5, 5},
		{"Period_10to6", 5, 5, 3, 3, 3, 3},
		{"Period_3to7", 1, 2, 3, 4, 3, 4},
		{"Period_Asym2T4F_to_5T3F", 2, 4, 5, 3, 5, 3},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tune := autoTune{}
			oldPeriod := tc.oldTrueDuration + tc.oldFalseDuration
			newPeriod := tc.newTrueDuration + tc.newFalseDuration

			// Phase 1: Fill buffer completely with old period signal
			for i := range maxAutoTuneSamples {
				posInPeriod := i % oldPeriod
				bit := posInPeriod < tc.oldTrueDuration
				tune.Sample(bit, uint32(i))
			}

			oldFoundTrue := tune.FindPeriod(true)
			oldFoundFalse := tune.FindPeriod(false)
			t.Logf("Old period: True=%d (expected %d), False=%d (expected %d)",
				oldFoundTrue, tc.oldTrueDuration, oldFoundFalse, tc.oldFalseDuration)

			assert.Equal(t, tc.oldTrueDuration, oldFoundTrue, "Old true period should match")
			assert.Equal(t, tc.oldFalseDuration, oldFoundFalse, "Old false period should match")

			// Phase 2: Overwrite with new period signal
			// Fill buffer completely to ensure all old data is replaced
			baseSeq := uint32(maxAutoTuneSamples)
			for i := range maxAutoTuneSamples {
				posInPeriod := i % newPeriod
				bit := posInPeriod < tc.newTrueDuration
				tune.Sample(bit, baseSeq+uint32(i))
			}

			newFoundTrue := tune.FindPeriod(true)
			newFoundFalse := tune.FindPeriod(false)
			t.Logf("New period: True=%d (expected %d), False=%d (expected %d)",
				newFoundTrue, tc.expectedNewTrue, newFoundFalse, tc.expectedNewFalse)

			assert.Equal(t, tc.expectedNewTrue, newFoundTrue,
				"New true period should match after overwrite")
			assert.Equal(t, tc.expectedNewFalse, newFoundFalse,
				"New false period should match after overwrite")
		})
	}
}

// TestAutoTuneMultiplePeriodChanges tests multiple consecutive period changes
func TestAutoTuneMultiplePeriodChanges(t *testing.T) {
	tune := autoTune{}

	// Define a sequence of periods to test
	periodSequence := []struct {
		trueDuration  int
		falseDuration int
	}{
		{2, 2}, // Period 4
		{3, 3}, // Period 6
		{5, 5}, // Period 10
		{1, 1}, // Period 2
		{4, 4}, // Period 8
		{2, 6}, // Period 8 (asymmetric)
		{7, 3}, // Period 10 (asymmetric)
	}

	seq := uint32(0)
	for phase, p := range periodSequence {
		period := p.trueDuration + p.falseDuration

		// Fill buffer completely with current period
		for i := range maxAutoTuneSamples {
			posInPeriod := i % period
			bit := posInPeriod < p.trueDuration
			tune.Sample(bit, seq)
			seq++
		}

		foundTrue := tune.FindPeriod(true)
		foundFalse := tune.FindPeriod(false)

		t.Logf("Phase %d: Period=%d (%dT+%dF), FoundTrue=%d, FoundFalse=%d",
			phase+1, period, p.trueDuration, p.falseDuration, foundTrue, foundFalse)

		assert.Equal(t, p.trueDuration, foundTrue,
			"Phase %d: True period should match", phase+1)
		assert.Equal(t, p.falseDuration, foundFalse,
			"Phase %d: False period should match", phase+1)
	}
}

// TestAutoTuneGradualPeriodTransition tests the transition period
// when old data is being gradually replaced by new data
func TestAutoTuneGradualPeriodTransition(t *testing.T) {
	tune := autoTune{}

	// First: fill with period 4 (2T+2F)
	oldTrueDuration := 2
	oldFalseDuration := 2
	oldPeriod := 4
	for i := range maxAutoTuneSamples {
		bit := (i % oldPeriod) < oldTrueDuration
		tune.Sample(bit, uint32(i))
	}

	// Verify old period for both true and false
	assert.Equal(t, oldTrueDuration, tune.FindPeriod(true))
	assert.Equal(t, oldFalseDuration, tune.FindPeriod(false))
	t.Logf("Initial period 4 (2T+2F) established: true=%d, false=%d",
		tune.FindPeriod(true), tune.FindPeriod(false))

	// Now gradually replace with period 6 (3T+3F)
	newTrueDuration := 3
	newFalseDuration := 3
	newPeriod := 6
	baseSeq := uint32(maxAutoTuneSamples)

	// Track when the new period becomes stable
	transitionCompleteTrue := -1
	transitionCompleteFalse := -1
	for i := range maxAutoTuneSamples {
		bit := (i % newPeriod) < newTrueDuration
		tune.Sample(bit, baseSeq+uint32(i))

		// Check every 10 samples
		if i%10 == 0 && i > 0 {
			foundTrue := tune.FindPeriod(true)
			foundFalse := tune.FindPeriod(false)
			// Once we find the new period, mark transition complete
			if foundTrue == newTrueDuration && transitionCompleteTrue == -1 {
				transitionCompleteTrue = i
				t.Logf("True transition complete at sample %d, found new period=%d", i, foundTrue)
			}
			if foundFalse == newFalseDuration && transitionCompleteFalse == -1 {
				transitionCompleteFalse = i
				t.Logf("False transition complete at sample %d, found new period=%d", i, foundFalse)
			}
		}
	}

	// After full overwrite, should definitely find new period for both
	finalPeriodTrue := tune.FindPeriod(true)
	finalPeriodFalse := tune.FindPeriod(false)
	assert.Equal(t, newTrueDuration, finalPeriodTrue,
		"After full overwrite, should find new true period")
	assert.Equal(t, newFalseDuration, finalPeriodFalse,
		"After full overwrite, should find new false period")
	t.Logf("Final period after full overwrite: true=%d, false=%d", finalPeriodTrue, finalPeriodFalse)
}

// TestAutoTuneExactPeriodAfterWrap tests that exact periods can be detected
// after the ring buffer has wrapped around
func TestAutoTuneExactPeriodAfterWrap(t *testing.T) {
	// Test specific patterns where we know the exact expected output
	testCases := []struct {
		name          string
		trueDuration  int
		falseDuration int
		expectedTrue  int
		expectedFalse int
	}{
		{"1T_1F", 1, 1, 1, 1},
		{"2T_2F", 2, 2, 2, 2},
		{"3T_3F", 3, 3, 3, 3},
		{"4T_4F", 4, 4, 4, 4},
		{"5T_5F", 5, 5, 5, 5},
		{"2T_4F", 2, 4, 2, 4},
		{"4T_2F", 4, 2, 4, 2},
		{"3T_6F", 3, 6, 3, 6},
		{"6T_3F", 6, 3, 6, 3},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tune := autoTune{}
			period := tc.trueDuration + tc.falseDuration

			// Generate enough samples to wrap around multiple times
			totalSamples := maxAutoTuneSamples * 3
			for i := range totalSamples {
				posInPeriod := i % period
				bit := posInPeriod < tc.trueDuration
				tune.Sample(bit, uint32(i))
			}

			assert.Equal(t, maxAutoTuneSamples, tune.count)

			periodTrue := tune.FindPeriod(true)
			periodFalse := tune.FindPeriod(false)

			t.Logf("Pattern %s: FoundTrue=%d (expected %d), FoundFalse=%d (expected %d)",
				tc.name, periodTrue, tc.expectedTrue, periodFalse, tc.expectedFalse)

			assert.Equal(t, tc.expectedTrue, periodTrue,
				"True period should match expected")
			assert.Equal(t, tc.expectedFalse, periodFalse,
				"False period should match expected")
		})
	}
}

// TestAutoTuneSequenceWrapAround tests period detection when sequence numbers wrap around
func TestAutoTuneSequenceWrapAround(t *testing.T) {
	tune := autoTune{}
	period := 4
	trueDuration := 2

	// Start from a high sequence number close to uint32 max
	startSeq := uint32(0xFFFFFFFF - 100)

	// Generate samples that will cause sequence wrap-around
	for i := range maxAutoTuneSamples + 50 {
		posInPeriod := i % period
		bit := posInPeriod < trueDuration
		tune.Sample(bit, startSeq+uint32(i))
	}

	periodTrue := tune.FindPeriod(true)
	periodFalse := tune.FindPeriod(false)

	t.Logf("Sequence wrap-around test: FoundTrue=%d, FoundFalse=%d", periodTrue, periodFalse)
	assert.Equal(t, 2, periodTrue)
	assert.Equal(t, 2, periodFalse)
}

// TestAutoTuneWithMissingAndDuplicateSamples checks that loss and identical
// duplicates do not prevent detection of later complete pulses.
func TestAutoTuneWithMissingAndDuplicateSamples(t *testing.T) {
	tune := autoTune{}
	for seq := range uint32(20) {
		if seq == 1 || seq == 6 || seq == 9 {
			continue
		}
		bit := seq%5 < 3
		tune.Sample(bit, seq)
		if seq == 5 {
			tune.Sample(bit, seq)
		}
	}

	assert.Equal(t, 3, tune.FindPeriod(true))
	assert.Equal(t, 2, tune.FindPeriod(false))
}

// TestAutoTuneFindsPulseAfterMissingBoundary checks recovery after a missing
// sample obscures an earlier pulse or one of its boundaries.
func TestAutoTuneFindsPulseAfterMissingBoundary(t *testing.T) {
	tests := []struct {
		name        string
		missingSeqs []uint32
		wantData    int
		wantParity  int
	}{
		{
			name:        "missing sample inside pulse",
			missingSeqs: []uint32{1, 7},
			wantData:    3,
			wantParity:  2,
		},
		{
			name:        "missing first pulse end boundary",
			missingSeqs: []uint32{3},
			wantData:    3,
			wantParity:  2,
		},
		{
			name:        "missing first pulse start boundary",
			missingSeqs: []uint32{5},
			wantData:    3,
			wantParity:  2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tune := autoTune{}
			for seq := range uint32(30) {
				missing := slices.Contains(tt.missingSeqs, seq)
				if missing {
					continue
				}
				tune.Sample(seq%5 < 3, seq)
			}

			assert.Equal(t, tt.wantData, tune.FindPeriod(true))
			assert.Equal(t, tt.wantParity, tune.FindPeriod(false))
		})
	}
}

// TestAutoTuneDiscardsConflictingDuplicateSequence checks that conflicting
// samples are ignored while later complete pulses remain detectable.
func TestAutoTuneDiscardsConflictingDuplicateSequence(t *testing.T) {
	tune := autoTune{}
	for seq := range uint32(30) {
		bit := seq%5 < 3
		tune.Sample(bit, seq)
		tune.Sample(bit, seq)
		if seq == 5 || seq == 10 {
			tune.Sample(!bit, seq)
		}
	}

	assert.Equal(t, 3, tune.FindPeriod(true))
	assert.Equal(t, 2, tune.FindPeriod(false))
}

// TestAutoTuneDoesNotInferPulseAcrossMissingBoundary rejects transitions
// inferred from samples whose sequence numbers are not adjacent.
func TestAutoTuneDoesNotInferPulseAcrossMissingBoundary(t *testing.T) {
	tune := autoTune{}
	for seq, bit := range []bool{false, true, true, false, false} {
		if seq == 1 || seq == 4 {
			continue
		}
		tune.Sample(bit, uint32(seq*2))
	}

	assert.Equal(t, -1, tune.FindPeriod(true))
	assert.Equal(t, -1, tune.FindPeriod(false))
}

// TestAutoTuneDoesNotInferPulseAcrossInternalGap rejects an apparent pulse
// whose interior contains an unobserved sequence number.
func TestAutoTuneDoesNotInferPulseAcrossInternalGap(t *testing.T) {
	tune := autoTune{}
	tune.Sample(false, 0)
	tune.Sample(true, 1)
	// Sequence 2 is a missing parity shard. Treating the data samples at
	// sequences 1 and 3 as one pulse would incorrectly infer a width of 3.
	tune.Sample(true, 3)
	tune.Sample(false, 4)

	assert.Equal(t, -1, tune.FindPeriod(true))
}

// TestAutoTuneExhaustiveEightSampleSignalAndLossPatterns compares all
// eight-position signal and reception masks in forward and reverse order.
func TestAutoTuneExhaustiveEightSampleSignalAndLossPatterns(t *testing.T) {
	const windowSize = 8
	const patternCount = 1 << windowSize

	startSeqs := []uint32{
		0,
		^uint32(0) - uint32(windowSize) + 1,
	}

	for _, startSeq := range startSeqs {
		for signalPattern := range patternCount {
			for receivedPattern := range patternCount {
				for _, reverse := range []bool{false, true} {
					tune := autoTune{}
					for offset := range windowSize {
						i := offset
						if reverse {
							i = windowSize - 1 - offset
						}
						if receivedPattern&(1<<i) != 0 {
							tune.Sample(signalPattern&(1<<i) != 0, startSeq+uint32(i))
						}
					}

					for _, bit := range []bool{false, true} {
						got := tune.FindPeriod(bit)
						want := completePulsePeriod(signalPattern, receivedPattern, windowSize, bit)
						if got != want {
							t.Fatalf(
								"start=%d reverse=%t signals=%08b received=%08b bit=%t: got period %d, want %d",
								startSeq, reverse, signalPattern, receivedPattern, bit, got, want,
							)
						}
					}
				}
			}
		}
	}
}

// TestAutoTuneExhaustiveEightSampleGapPatterns compares all eight-sample
// signal patterns and combinations of adjacent or separated sequences.
func TestAutoTuneExhaustiveEightSampleGapPatterns(t *testing.T) {
	const (
		sampleCount = 8
		signalCount = 1 << sampleCount
		gapCount    = 1 << (sampleCount - 1)
	)

	startSeqs := []uint32{
		0,
		^uint32(0) - uint32(2*(sampleCount-1)) + 1,
	}

	for _, startSeq := range startSeqs {
		for signalPattern := range signalCount {
			for gapPattern := range gapCount {
				samples := make([]pulse, sampleCount)
				samples[0] = pulse{
					bit: signalPattern&1 != 0,
					seq: startSeq,
				}
				for i := 1; i < sampleCount; i++ {
					step := uint32(1)
					if gapPattern&(1<<(i-1)) != 0 {
						// FindPeriod only distinguishes adjacent sequence
						// numbers from any gap, so 2 represents all gaps.
						step = 2
					}
					samples[i] = pulse{
						bit: signalPattern&(1<<i) != 0,
						seq: samples[i-1].seq + step,
					}
				}

				for _, reverse := range []bool{false, true} {
					tune := autoTune{}
					for offset := range sampleCount {
						i := offset
						if reverse {
							i = sampleCount - 1 - offset
						}
						tune.Sample(samples[i].bit, samples[i].seq)
					}

					for _, bit := range []bool{false, true} {
						got := tune.FindPeriod(bit)
						want := completePulsePeriodForSamples(samples, bit)
						if got != want {
							t.Fatalf(
								"start=%d reverse=%t signals=%08b gaps=%07b bit=%t: got period %d, want %d",
								startSeq, reverse, signalPattern, gapPattern, bit, got, want,
							)
						}
					}
				}
			}
		}
	}
}

// TestAutoTuneExhaustiveArrivalOrders checks every arrival permutation for
// each five-position signal and reception pattern.
func TestAutoTuneExhaustiveArrivalOrders(t *testing.T) {
	const (
		windowSize   = 5
		patternCount = 1 << windowSize
	)

	startSeqs := []uint32{
		0,
		^uint32(0) - uint32(windowSize) + 1,
	}

	for _, startSeq := range startSeqs {
		for signalPattern := range patternCount {
			for receivedPattern := range patternCount {
				indices := make([]int, 0, windowSize)
				for i := range windowSize {
					if receivedPattern&(1<<i) != 0 {
						indices = append(indices, i)
					}
				}

				forEachPermutation(indices, func(order []int) {
					tune := autoTune{}
					for _, i := range order {
						tune.Sample(signalPattern&(1<<i) != 0, startSeq+uint32(i))
					}

					for _, bit := range []bool{false, true} {
						got := tune.FindPeriod(bit)
						want := completePulsePeriod(signalPattern, receivedPattern, windowSize, bit)
						if got != want {
							t.Fatalf(
								"start=%d signals=%05b received=%05b order=%v bit=%t: got period %d, want %d",
								startSeq, signalPattern, receivedPattern, order, bit, got, want,
							)
						}
					}
				})
			}
		}
	}
}

// TestAutoTuneExhaustiveDuplicatePatterns covers missing, single, duplicate,
// and conflicting samples at every position of each five-position signal.
func TestAutoTuneExhaustiveDuplicatePatterns(t *testing.T) {
	const (
		windowSize       = 5
		signalCount      = 1 << windowSize
		sampleStateCount = 1 << (2 * windowSize)
	)

	startSeqs := []uint32{
		0,
		^uint32(0) - uint32(windowSize) + 1,
	}

	for _, startSeq := range startSeqs {
		for signalPattern := range signalCount {
			for sampleStates := range sampleStateCount {
				// Conflicting duplicates are treated as missing by the oracle.
				receivedPattern := 0
				for i := range windowSize {
					state := sampleStates >> (2 * i) & 3
					if state == 1 || state == 2 {
						receivedPattern |= 1 << i
					}
				}

				for _, reverse := range []bool{false, true} {
					tune := autoTune{}
					for offset := range windowSize {
						i := offset
						if reverse {
							i = windowSize - 1 - offset
						}

						state := sampleStates >> (2 * i) & 3
						bit := signalPattern&(1<<i) != 0
						seq := startSeq + uint32(i)
						switch state {
						case 1:
							tune.Sample(bit, seq)
						case 2:
							tune.Sample(bit, seq)
							tune.Sample(bit, seq)
						case 3:
							tune.Sample(bit, seq)
							tune.Sample(!bit, seq)
						}
					}

					for _, bit := range []bool{false, true} {
						got := tune.FindPeriod(bit)
						want := completePulsePeriod(signalPattern, receivedPattern, windowSize, bit)
						if got != want {
							t.Fatalf(
								"start=%d reverse=%t signals=%05b states=%010b bit=%t: got period %d, want %d",
								startSeq, reverse, signalPattern, sampleStates, bit, got, want,
							)
						}
					}
				}
			}
		}
	}
}

// forEachPermutation invokes fn for each ordering, reusing the input slice.
func forEachPermutation(values []int, fn func([]int)) {
	var permute func(int)
	permute = func(index int) {
		if index == len(values) {
			fn(values)
			return
		}

		for i := index; i < len(values); i++ {
			values[index], values[i] = values[i], values[index]
			permute(index + 1)
			values[index], values[i] = values[i], values[index]
		}
	}
	permute(0)
}

// completePulsePeriod is the test oracle for a signal and reception bitmask.
func completePulsePeriod(signalPattern, receivedPattern, windowSize int, bit bool) int {
	bitAt := func(i int) bool {
		return signalPattern&(1<<i) != 0
	}
	receivedAt := func(i int) bool {
		return receivedPattern&(1<<i) != 0
	}

	for left := 1; left < windowSize; left++ {
		if !receivedAt(left-1) || !receivedAt(left) ||
			bitAt(left-1) == bit || bitAt(left) != bit {
			continue
		}

		for right := left + 1; right < windowSize; right++ {
			if !receivedAt(right) {
				break
			}
			if bitAt(right) != bit {
				return right - left
			}
		}
	}

	return -1
}

// completePulsePeriodForSamples is the test oracle for sequence-ordered
// samples that may have gaps or wrap around uint32.
func completePulsePeriodForSamples(samples []pulse, bit bool) int {
	for left := 1; left < len(samples); left++ {
		if samples[left-1].bit == bit || samples[left].bit != bit ||
			samples[left-1].seq+1 != samples[left].seq {
			continue
		}

		for right := left + 1; right < len(samples); right++ {
			if samples[right-1].seq+1 != samples[right].seq {
				break
			}
			if samples[right].bit != bit {
				return int(_itimediff(samples[right].seq, samples[left].seq))
			}
		}
	}

	return -1
}

// TestFECDecoderAutoTunesConfigurationMatrix checks reconfiguration across
// shard counts and injected loss and duplicate scenarios.
func TestFECDecoderAutoTunesConfigurationMatrix(t *testing.T) {
	scenarios := []struct {
		name      string
		missing   bool
		duplicate bool
	}{
		{name: "clean"},
		{name: "duplicate_data", duplicate: true},
		{name: "missing_shard", missing: true},
		{name: "missing_and_duplicate", missing: true, duplicate: true},
	}
	configurations := make([]struct {
		dataShards   int
		parityShards int
	}, 0, 272)
	for dataShards := 1; dataShards <= 16; dataShards++ {
		for parityShards := 1; parityShards <= 16; parityShards++ {
			configurations = append(configurations, struct {
				dataShards   int
				parityShards int
			}{dataShards, parityShards})
		}
	}
	configurations = append(configurations,
		struct {
			dataShards   int
			parityShards int
		}{32, 1},
		struct {
			dataShards   int
			parityShards int
		}{1, 32},
		struct {
			dataShards   int
			parityShards int
		}{127, 1},
		struct {
			dataShards   int
			parityShards int
		}{1, 127},
		struct {
			dataShards   int
			parityShards int
		}{255, 1},
		struct {
			dataShards   int
			parityShards int
		}{1, 255},
	)

	for _, tc := range configurations {
		for _, scenario := range scenarios {
			t.Run(
				fmt.Sprintf("%d-data-%d-parity/%s", tc.dataShards, tc.parityShards, scenario.name),
				func(t *testing.T) {
					encoder := newFECEncoder(tc.dataShards, tc.parityShards, 0)
					decoder := newFECDecoder(2, 2)
					if encoder == nil || decoder == nil {
						t.Fatal("failed to create FEC encoder or decoder")
					}
					t.Cleanup(func() {
						logAutoTunePattern(t, &decoder.autoTune)
					})

					dropDataShard, dropParityShard, duplicateDataShard := -1, -1, -1
					if scenario.missing {
						if tc.dataShards > 1 {
							dropDataShard = 1
						} else {
							dropParityShard = min(1, tc.parityShards-1)
						}
					}
					if scenario.duplicate {
						duplicateDataShard = tc.dataShards - 1
						if duplicateDataShard == dropDataShard {
							duplicateDataShard = 0
						}
					}
					feedFECGroupWithMutations(t, encoder, decoder, dropDataShard, dropParityShard, duplicateDataShard)
					if scenario.missing || scenario.duplicate {
						t.Log("pattern after injected anomalies:")
						logAutoTunePattern(t, &decoder.autoTune)
					}
					feedFECGroup(t, encoder, decoder, -1)
					if scenario.missing {
						// A 1-data/1-parity configuration needs another data
						// edge after the recovered parity pulse.
						feedFECGroup(t, encoder, decoder, -1)
					}

					if decoder.dataShards != tc.dataShards || decoder.parityShards != tc.parityShards {
						t.Fatalf(
							"auto-tuned to %d data/%d parity, want %d data/%d parity",
							decoder.dataShards, decoder.parityShards, tc.dataShards, tc.parityShards,
						)
					}
				},
			)
		}
	}
}

// TestFECDecoderAutoTunesAfterIncompleteGroup verifies convergence after loss
// makes the first shard group ambiguous.
func TestFECDecoderAutoTunesAfterIncompleteGroup(t *testing.T) {
	encoder := newFECEncoder(4, 2, 0)
	decoder := newFECDecoder(1, 1)
	if encoder == nil || decoder == nil {
		t.Fatal("failed to create FEC encoder or decoder")
	}

	// Drop a data shard from the first group. The decoder must ignore its
	// ambiguous pulse and tune from the following complete group.
	feedFECGroup(t, encoder, decoder, 2)
	feedFECGroup(t, encoder, decoder, -1)

	if decoder.dataShards != 4 || decoder.parityShards != 2 {
		t.Fatalf(
			"auto-tuned to %d data/%d parity, want 4 data/2 parity",
			decoder.dataShards, decoder.parityShards,
		)
	}
}

// TestFECDecoderRejectsAutoTuneConfigurationOverShardLimit ensures an
// inferred configuration exceeding 256 shards is not applied.
func TestFECDecoderRejectsAutoTuneConfigurationOverShardLimit(t *testing.T) {
	decoder := newFECDecoder(2, 2)
	if decoder == nil {
		t.Fatal("failed to create FEC decoder")
	}

	decoder.autoTune.Sample(false, 0)
	for seq := uint32(1); seq <= 256; seq++ {
		decoder.autoTune.Sample(true, seq)
	}
	decoder.autoTune.Sample(false, 257)

	packet := make([]byte, fecHeaderSize)
	binary.LittleEndian.PutUint32(packet, 258)
	binary.LittleEndian.PutUint16(packet[4:], typeData)
	decoder.decode(packet)

	if decoder.dataShards != 2 || decoder.parityShards != 2 {
		t.Fatalf(
			"accepted invalid auto-tuned configuration %d data/%d parity",
			decoder.dataShards, decoder.parityShards,
		)
	}
}

func feedFECGroup(t *testing.T, encoder *fecEncoder, decoder *fecDecoder, dropDataShard int) {
	feedFECGroupWithMutations(t, encoder, decoder, dropDataShard, -1, -1)
}

func feedFECGroupWithDuplicate(t *testing.T, encoder *fecEncoder, decoder *fecDecoder, dropDataShard, duplicateDataShard int) {
	feedFECGroupWithMutations(t, encoder, decoder, dropDataShard, -1, duplicateDataShard)
}

// feedFECGroupWithMutations sends one encoded group, optionally dropping or
// duplicating indexed shards; -1 disables the corresponding mutation.
func feedFECGroupWithMutations(t *testing.T, encoder *fecEncoder, decoder *fecDecoder, dropDataShard, dropParityShard, duplicateDataShard int) {
	t.Helper()
	encoder.tsLatestPacket = time.Now().UnixMilli()
	for dataShard := range encoder.dataShards {
		packet := make([]byte, fecPacketHeaderSize+32)
		parity := encoder.encode(packet, ^uint32(0))
		if dataShard != dropDataShard {
			recycleRecoveredShards(decoder.decode(packet))
			if dataShard == duplicateDataShard {
				recycleRecoveredShards(decoder.decode(packet))
			}
		}
		for parityShard, shard := range parity {
			if parityShard == dropParityShard {
				continue
			}
			recycleRecoveredShards(decoder.decode(fecPacket(shard)))
		}
	}
}

func recycleRecoveredShards(shards [][]byte) {
	for _, shard := range shards {
		defaultBufferPool.Put(shard)
	}
}

// TestAutoTuneSupports256Shards verifies the largest supported shard set can
// be inferred and applied by the decoder.
func TestAutoTuneSupports256Shards(t *testing.T) {
	decoder := newFECDecoder(1, 1)
	if decoder == nil {
		t.Fatal("failed to create decoder")
	}

	for seq := range uint32(257) {
		decoder.autoTune.Sample(seq > 0 && seq < 256, seq)
	}
	packet := make([]byte, fecHeaderSize)
	binary.LittleEndian.PutUint32(packet, 257)
	binary.LittleEndian.PutUint16(packet[4:], typeData)
	decoder.decode(packet)

	assert.Equal(t, 255, decoder.dataShards)
	assert.Equal(t, 1, decoder.parityShards)
	assert.Equal(t, 256, decoder.shardSize)
}
