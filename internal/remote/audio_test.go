package remote

import (
	"context"
	"encoding/binary"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRXAudioDecodesAuthenticatedPCM(t *testing.T) {
	audio := &RXAudio{}
	server := httptest.NewServer(Handler(&testRadio{}, func(key string) bool {
		return key == "test-password"
	}, func() {}, audio, nil, nil))
	defer server.Close()
	client := Connect(strings.TrimPrefix(server.URL, "http://"), "test-password")
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		tick := time.NewTicker(10 * time.Millisecond)
		defer tick.Stop()
		var block [AudioSampleRate / 50 * 2]byte
		binary.LittleEndian.PutUint16(block[:2], 16384)
		binary.LittleEndian.PutUint16(block[2:4], 49152)
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				audio.Publish(block[:])
			}
		}
	}()
	var received bool
	err := client.PlayRX(ctx, func(samples []float32) {
		received = len(samples) == 960 && samples[0] == .5 && samples[1] == -.5
		cancel()
	})
	<-done
	if !received {
		t.Fatalf("did not receive decoded 48 kHz PCM: %v", err)
	}
}
