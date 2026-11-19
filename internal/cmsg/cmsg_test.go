package cmsg

import (
	"errors"
	"testing"
)

type failingRawConn struct {
	err error
}

// Control returns the configured control error, or calls f with an invalid descriptor.
func (c failingRawConn) Control(f func(uintptr)) error {
	if c.err != nil {
		return c.err
	}
	f(^uintptr(0))
	return nil
}

// Read is not used by control-message initialization.
func (c failingRawConn) Read(func(uintptr) bool) error { return errors.New("unexpected Read") }

// Write is not used by control-message initialization.
func (c failingRawConn) Write(func(uintptr) bool) error { return errors.New("unexpected Write") }

// TestSetupControlMessageErrors checks raw-connection and socket-option failures.
func TestSetupControlMessageErrors(t *testing.T) {
	if !SupportsControlMessage {
		t.Skip("destination control messages are not supported on this platform")
	}
	controlErr := errors.New("control failed")
	if err := SetupControlMessage(failingRawConn{err: controlErr}); !errors.Is(err, controlErr) {
		t.Fatalf("control error = %v, want %v", err, controlErr)
	}
	if err := SetupControlMessage(failingRawConn{}); err == nil {
		t.Fatal("expected an error when both socket options fail")
	}
}

// TestParseControlMessageEmpty checks that missing destination metadata is rejected.
func TestParseControlMessageEmpty(t *testing.T) {
	if !SupportsControlMessage {
		t.Skip("destination control messages are not supported on this platform")
	}
	if _, err := ParseControlMessage(nil); err == nil {
		t.Fatal("expected an error for missing control messages")
	}
}
