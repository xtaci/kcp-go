package kcp

const maxAutoTuneSamples = 258

// pulse represents a 0/1 signal with time sequence
type pulse struct {
	bit bool   // 0 or 1
	seq uint32 // sequence of the signal
}

// autoTune object
type autoTune struct {
	pulses [maxAutoTuneSamples]pulse
}

// Sample adds a signal sample to the pulse buffer
func (tune *autoTune) Sample(bit bool, seq uint32) {
	tune.pulses[seq%maxAutoTuneSamples] = pulse{bit, seq}
}

// Find a period for a given signal
// returns -1 if not found
//
//    ---              ------
//      |              |
//      |______________|
//          Period
//  Falling Edge    Rising Edge
func (tune *autoTune) FindPeriod(bit bool) int {
	// find left and right edges for a bit
	lastPulse := tune.pulses[0]
	idx := 1

	// left edge
	var leftEdge int
	for ; idx < len(tune.pulses); idx++ {
		if lastPulse.seq+1 == tune.pulses[idx].seq {
			if lastPulse.bit != bit && tune.pulses[idx].bit == bit {
				leftEdge = idx
				break
			}
		} else {
			return -1
		}
		lastPulse = tune.pulses[idx]
	}

	// right edge
	var rightEdge int
	lastPulse = tune.pulses[leftEdge]
	idx = leftEdge + 1

	for ; idx < len(tune.pulses); idx++ {
		if lastPulse.seq+1 == tune.pulses[idx].seq {
			if lastPulse.bit == bit && tune.pulses[idx].bit != bit {
				rightEdge = idx
				break
			}
		} else {
			return -1
		}
		lastPulse = tune.pulses[idx]
	}

	return rightEdge - leftEdge
}
