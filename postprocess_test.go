package kcp

import (
	"testing"
	"time"
)

// Rate limited sessions waiting for tokens must not hold the shared
// postProcess workers from a session without a limit.
func TestPostProcessRateLimitedMixed(t *testing.T) {
	port := nextPort()
	l := sinkServer(port)
	defer l.Close()

	// More limited sessions than workers, each with more queued than its
	// limit lets out.
	stop := make(chan struct{})
	defer close(stop)
	for i := 0; i < 16; i++ {
		s, err := dialSink(port)
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		s.SetRateLimit(32 * 1024)
		go func() {
			buf := make([]byte, 64*1024)
			for {
				select {
				case <-stop:
					return
				default:
				}
				if _, err := s.Write(buf); err != nil {
					return
				}
			}
		}()
	}
	// Let the limiters spend their bursts.
	time.Sleep(time.Second)

	eport := nextPort()
	el := echoServer(eport, nil)
	defer el.Close()
	cli, err := dialEcho(eport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()
	cli.SetRateLimit(0)
	cli.SetLogger(0, nil)

	start := time.Now()
	if err := echo_tester(cli, 1024, 256); err != nil {
		t.Fatal(err)
	}
	took := time.Since(start)
	t.Logf("unlimited session echoed 256 KiB in %v", took)
	if took > 2*time.Second {
		t.Fatalf("unlimited session took %v behind rate limited ones", took)
	}
}
