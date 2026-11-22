package remote

import (
	"context"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LuislopezMartinez/IC-RADIO/internal/civ"
	"github.com/LuislopezMartinez/IC-RADIO/internal/swr"
)

type sweepTestRadio struct {
	mu            sync.Mutex
	state         civ.State
	keyed         chan bool
	restored      chan struct{}
	pointFailures bool
	attempt       int
}

func (r *sweepTestRadio) Snapshot() civ.State {
	r.mu.Lock()
	defer r.mu.Unlock()
	state := r.state
	state.LastReply, state.TXAt = time.Now(), time.Now().Add(time.Millisecond)
	if r.pointFailures && state.TX {
		state.Meters[0x11], state.MeterAt[0x11] = 60, state.LastReply
		if r.attempt >= 3 {
			state.Meters[0x12], state.MeterAt[0x12] = 48, state.LastReply
		}
	}
	return state
}

func (r *sweepTestRadio) MeasureSWR(active bool) {
	if !active {
		close(r.restored)
	}
}

func (r *sweepTestRadio) ReadPower() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	replyAt := time.Now().Add(time.Millisecond)
	r.state.PowerAt, r.state.SplitAt = replyAt, replyAt
	return true
}

func (r *sweepTestRadio) SetPower(power int) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.state.PowerSetting, r.state.PowerAt = power, time.Now().Add(time.Millisecond)
	return true
}

func (r *sweepTestRadio) Tune(hz int64) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.state.Frequency, r.state.FrequencyAt = hz, time.Now().Add(time.Millisecond)
	return true
}

func (r *sweepTestRadio) TX() bool { return r.Snapshot().TX }

func (r *sweepTestRadio) SetTX(on bool) error {
	r.mu.Lock()
	if on {
		r.attempt++
	}
	r.state.TX = on && (!r.pointFailures || r.attempt > 1)
	r.mu.Unlock()
	r.keyed <- on
	return nil
}

func TestRemoteSweepContinuesAfterMissingPoints(t *testing.T) {
	radio := &sweepTestRadio{
		state: civ.State{Frequency: 18145000, Mode: 1, ModeKnown: true, TXKnown: true, PowerSetting: 109},
		keyed: make(chan bool, 8), restored: make(chan struct{}), pointFailures: true,
	}
	tx := &TXAudio{
		Ready:         func() bool { return true },
		StartPlayback: func() error { return nil },
		StopPlayback:  func() {},
		Write:         func([]float32) {},
	}
	server := httptest.NewServer(Handler(&testRadio{}, func(key string) bool {
		return key == "test-password"
	}, func() {}, nil, tx, tx.SweepHandler(radio, radio)))
	defer server.Close()
	client := Connect(strings.TrimPrefix(server.URL, "http://"), "test-password")
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	var final swr.Report
	err := client.Sweep(ctx, swr.Request{Center: 18145000, Step: 50000, Count: 3}, func(report swr.Report) {
		if report.Final {
			final = report
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(final.Points) != 3 || final.Points[0].Failure != swr.NoTransmission ||
		final.Points[1].Failure != swr.NoReading || !final.Points[2].Valid {
		t.Fatalf("unexpected results after missing measurements: %+v", final)
	}
	state := radio.Snapshot()
	if state.TX || state.Frequency != 18145000 || state.PowerSetting != 109 {
		t.Fatalf("radio was not restored: %+v", state)
	}
}

func (r *sweepTestRadio) Close() {}

func TestRemoteSweepCancellationReleasesAndRestoresRadio(t *testing.T) {
	radio := &sweepTestRadio{
		state: civ.State{Frequency: 14200000, Mode: 1, ModeKnown: true, TXKnown: true, PowerSetting: 109},
		keyed: make(chan bool, 4), restored: make(chan struct{}),
	}
	tx := &TXAudio{
		Ready:         func() bool { return true },
		StartPlayback: func() error { return nil },
		StopPlayback:  func() {},
		Write:         func([]float32) {},
	}
	server := httptest.NewServer(Handler(&testRadio{}, func(key string) bool {
		return key == "test-password"
	}, func() {}, nil, tx, tx.SweepHandler(radio, radio)))
	defer server.Close()
	client := Connect(strings.TrimPrefix(server.URL, "http://"), "test-password")
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	reports := make(chan swr.Report, 16)
	done := make(chan error, 1)
	go func() {
		done <- client.Sweep(ctx, swr.Request{Center: 14200000, Step: 10000, Count: 3}, func(report swr.Report) {
			reports <- report
		})
	}()
	select {
	case report := <-reports:
		if len(report.Points) != 3 || report.Final {
			t.Fatalf("unexpected first report: %+v", report)
		}
	case err := <-done:
		t.Fatalf("sweep ended before delivering progress: %v", err)
	case <-ctx.Done():
		t.Fatal("no sweep progress")
	}
	select {
	case on := <-radio.keyed:
		if !on {
			t.Fatal("PTT was not keyed")
		}
	case <-ctx.Done():
		t.Fatal("no PTT event")
	}
	cancel()
	select {
	case <-radio.restored:
	case <-time.After(time.Second):
		t.Fatal("radio was not restored after cancellation")
	}
	state := radio.Snapshot()
	if state.TX || state.Frequency != 14200000 || state.PowerSetting != 109 {
		t.Fatalf("radio not restored: %+v", state)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("remote client did not stop")
	}
}
