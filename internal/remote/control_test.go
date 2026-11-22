package remote

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/LuislopezMartinez/IC-RADIO/internal/civ"
)

type testRadio struct {
	Radio
	wake chan struct{}
	tune chan int64
}

func (r *testRadio) EnsureRadioOn() bool {
	r.wake <- struct{}{}
	return true
}

func (r *testRadio) Tune(hz int64) bool {
	r.tune <- hz
	return true
}

func (r *testRadio) TakeDisplaySnapshot() (civ.State, [civ.Samples]byte) {
	state := civ.State{Frequency: 14200000, ModeKnown: true, LastReply: time.Now()}
	state.Sweep = civ.Sweep{Low: 14150000, High: 14250000}
	return state, [civ.Samples]byte{37}
}

func TestLoopbackControl(t *testing.T) {
	radio := &testRadio{wake: make(chan struct{}, 1), tune: make(chan int64, 1)}
	activity := make(chan struct{}, 1)
	server := httptest.NewServer(Handler(radio, func(password string) bool {
		return password == "test-password"
	}, func() {
		select {
		case activity <- struct{}{}:
		default:
		}
	}, nil, nil, nil))
	defer server.Close()
	endpoint := strings.TrimPrefix(server.URL, "http://")
	rejected := Connect(endpoint, "wrong-password")
	if !rejected.EnsureRadioOn() {
		t.Fatal("could not queue command")
	}
	deadline := time.Now().Add(time.Second)
	for rejected.Snapshot().Error != PasswordError && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	rejected.Close()
	if rejected.Snapshot().Error != PasswordError || len(radio.wake) != 0 || len(activity) != 0 {
		t.Fatal("invalid password was not rejected before the radio command and activity tracking")
	}
	client := Connect(endpoint, "test-password")
	defer client.Close()

	if !client.EnsureRadioOn() || !client.Tune(14201000) {
		t.Fatal("could not queue control commands")
	}
	select {
	case <-radio.wake:
	case <-time.After(time.Second):
		t.Fatal("wake did not reach the radio")
	}
	select {
	case got := <-radio.tune:
		if got != 14201000 {
			t.Fatalf("radio received frequency %d", got)
		}
	case <-time.After(time.Second):
		t.Fatal("tune did not reach the radio")
	}
	deadline = time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		state, peaks := client.TakeDisplaySnapshot()
		if state.ModeKnown && state.Frequency == 14200000 && state.Sweep.High == 14250000 && peaks[0] == 37 {
			if len(activity) == 0 {
				t.Fatal("authenticated requests did not report client activity")
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("client did not receive the radio state and scope data")
}
