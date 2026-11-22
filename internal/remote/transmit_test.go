package remote

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRemotePTTReleases(t *testing.T) {
	for _, tc := range []string{"disconnect", "microphone stops", "normal end of stream"} {
		t.Run(tc, func(t *testing.T) {
			ptt := make(chan bool, 4)
			writes := make(chan float32, 1)
			tx := &TXAudio{
				Ready:         func() bool { return true },
				Keyed:         func() bool { return true },
				Key:           func(on bool) error { ptt <- on; return nil },
				StartPlayback: func() error { return nil },
				StopPlayback:  func() {},
				Write:         func(samples []float32) { writes <- samples[0] },
			}
			server := httptest.NewServer(Handler(&testRadio{}, func(password string) bool {
				return password == "test-password"
			}, func() {}, nil, tx, nil))
			defer server.Close()
			client := Connect(strings.TrimPrefix(server.URL, "http://"), "test-password")
			defer client.Close()

			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			blocks := make(chan []byte, 1)
			pcm := make([]byte, TXBlockBytes)
			pcm[1] = 64
			blocks <- pcm
			done := make(chan error, 1)
			go func() { done <- client.Transmit(ctx, blocks) }()
			expectPTT := func(on bool, within time.Duration) {
				t.Helper()
				select {
				case got := <-ptt:
					if got != on {
						t.Fatalf("PTT = %v, want %v", got, on)
					}
				case <-time.After(within):
					t.Fatalf("PTT did not reach %v", on)
				}
			}
			expectPTT(true, time.Second)
			select {
			case got := <-writes:
				if got != .5 {
					t.Fatalf("decoded PCM sample = %v", got)
				}
			case <-time.After(time.Second):
				t.Fatal("microphone audio did not reach the server")
			}
			switch tc {
			case "disconnect":
				cancel()
			case "normal end of stream":
				close(blocks)
			}
			expectPTT(false, time.Second)
			select {
			case err := <-done:
				if tc == "normal end of stream" && err != nil {
					t.Fatalf("normal end failed: %v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("upload did not exit")
			}
		})
	}
}
