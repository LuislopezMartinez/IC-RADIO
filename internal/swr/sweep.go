package swr

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/LuislopezMartinez/IC-RADIO/internal/civ"
)

const swrPowerSetting = 77 // nearest CI-V setting to 30% of the IC-7300's 100 W range
const ToneHz = 1000
const swrMaxTX = 1500 * time.Millisecond

var Steps = [...]int64{10000, 50000, 100000, 500000}

type Request struct {
	Center int64 `json:"center"`
	Step   int64 `json:"step"`
	Count  int   `json:"count"`
}

func (r Request) ValidatedPoints() []Point {
	validStep := false
	for _, step := range Steps {
		validStep = validStep || r.Step == step
	}
	if !validStep || !civ.CanTune(r.Center) || r.Count < 3 || r.Count > 13 || r.Count%2 == 0 {
		return nil
	}
	points := Frequencies(r.Center, r.Step, r.Count)
	if !civ.CanTune(points[0].Hz-ToneHz) || !civ.CanTune(points[len(points)-1].Hz+ToneHz) {
		return nil
	}
	return points
}

type Point struct {
	Hz      int64        `json:"hz"`
	Raw     int          `json:"raw_swr"`
	Power   int          `json:"raw_po"`
	Valid   bool         `json:"valid"`
	Failure PointFailure `json:"failure,omitempty"`
}

type PointFailure string

const (
	NoTransmission PointFailure = "no_transmission"
	NoReading      PointFailure = "no_reading"
)

type Report struct {
	Points []Point `json:"points"`
	Status string  `json:"status"`
	Final  bool    `json:"final"`
}

type Radio interface {
	Snapshot() civ.State
	MeasureSWR(bool)
	ReadPower() bool
	SetPower(int) bool
	Tune(int64) bool
}

type Keyer interface {
	TX() bool
	SetTX(bool) error
	Close()
}

func Frequencies(center, step int64, count int) []Point {
	points := make([]Point, count)
	for i := range points {
		points[i].Hz = center + int64(i-count/2)*step
	}
	return points
}

func Ratio(raw int) float64 {
	switch {
	case raw <= 48:
		return 1 + float64(raw)/96
	case raw <= 80:
		return 1.5 + float64(raw-48)/64
	case raw <= 120:
		return 2 + float64(raw-80)/40
	default:
		return 3
	}
}

func swrWait(ctx context.Context, radio Radio, check func(civ.State) bool) (civ.State, bool) {
	tick := time.NewTicker(12 * time.Millisecond)
	defer tick.Stop()
	for ctx.Err() == nil {
		state := radio.Snapshot()
		if state.Error != "" || time.Since(state.LastReply) > 2*time.Second {
			break
		}
		if check(state) {
			return state, true
		}
		select {
		case <-ctx.Done():
		case <-tick.C:
		}
	}
	return civ.State{}, false
}

func swrWaitFor(ctx context.Context, radio Radio, timeout time.Duration, check func(civ.State) bool) (civ.State, bool) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return swrWait(ctx, radio, check)
}

func Run(ctx context.Context, radio Radio, keyer Keyer, setTone func(time.Duration), initial civ.State, points []Point, report func(Report)) {
	status := "Barrido completado"
	original := -1
	changedFrequency := false
	defer func() {
		setTone(0)
		releasedAt := time.Now()
		if keyer.TX() {
			if err := keyer.SetTX(false); err != nil {
				keyer.Close()
			}
		}
		if original >= 0 || changedFrequency {
			cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			_, rx := swrWait(cleanup, radio, func(s civ.State) bool { return s.TXAt.After(releasedAt) && !s.TX })
			restored := rx
			if rx && original >= 0 {
				setAt := time.Now()
				restored = radio.SetPower(original)
				if restored {
					_, restored = swrWaitFor(cleanup, radio, 1200*time.Millisecond, func(s civ.State) bool { return s.PowerAt.After(setAt) && s.PowerSetting == original })
				}
			}
			if rx && changedFrequency {
				at := time.Now()
				if radio.Tune(initial.Frequency) {
					_, tuned := swrWait(cleanup, radio, func(s civ.State) bool { return s.FrequencyAt.After(at) && s.Frequency == initial.Frequency })
					restored = restored && tuned
				} else {
					restored = false
				}
			}
			if !restored {
				status += " - comprueba frecuencia y potencia en el Icom"
			}
		}
		radio.MeasureSWR(false)
		if ctx.Err() != nil && status == "Barrido completado" {
			status = "Barrido detenido"
		}
		report(Report{Points: append([]Point(nil), points...), Status: status, Final: true})
	}()
	radio.MeasureSWR(true)
	at := time.Now()
	if !radio.ReadPower() {
		status = "No se pudo consultar la potencia"
		return
	}
	state, ok := swrWaitFor(ctx, radio, 1500*time.Millisecond, func(s civ.State) bool {
		return s.PowerAt.After(at) && s.SplitAt.After(at) && s.Tuning.DeltaTXAt.After(at) && s.TXKnown && !s.TX
	})
	if !ok || state.Split || state.Tuning.DeltaTX {
		status = "El test necesita RX con SPLIT y Delta TX desactivados"
		return
	}
	original = state.PowerSetting
	at = time.Now()
	if !radio.SetPower(swrPowerSetting) {
		status = "No se pudo ajustar la potencia"
		return
	}
	_, ok = swrWaitFor(ctx, radio, 1500*time.Millisecond, func(s civ.State) bool { return s.PowerAt.After(at) && s.PowerSetting == swrPowerSetting })
	if !ok {
		status = "El Icom no ha confirmado la potencia de prueba"
		return
	}
	for i := range points {
		if ctx.Err() != nil {
			return
		}
		point := &points[i]
		report(Report{Points: append([]Point(nil), points...), Status: fmt.Sprintf("Midiendo %d de %d", i+1, len(points))})
		if ctx.Err() != nil {
			return
		}
		vfo := point.Hz - ToneHz
		if initial.Mode == 0 {
			vfo = point.Hz + ToneHz
		}
		at = time.Now()
		if !radio.Tune(vfo) {
			status = "No se pudo solicitar la frecuencia de prueba"
			return
		}
		changedFrequency = true
		_, ok = swrWaitFor(ctx, radio, 1200*time.Millisecond, func(s civ.State) bool {
			return s.FrequencyAt.After(at) && s.Frequency == vfo && s.TXKnown && !s.TX && !s.Split && !s.Tuning.DeltaTX && s.Mode == initial.Mode
		})
		if !ok || ctx.Err() != nil {
			status = "El Icom no ha confirmado la frecuencia de prueba"
			return
		}
		if err := keyer.SetTX(true); err != nil {
			status = "No se pudo activar el PTT"
			return
		}
		keyed := time.Now()
		setTone(swrMaxTX)
		captured := swrCapture(ctx, radio, keyed, vfo, initial.Mode, point)
		setTone(0)
		releasedAt := time.Now()
		if err := keyer.SetTX(false); err != nil {
			status = "No se pudo liberar el PTT"
			keyer.Close()
			return
		}
		_, ok = swrWaitFor(ctx, radio, 1000*time.Millisecond, func(s civ.State) bool { return s.TXAt.After(releasedAt) && !s.TX })
		if ctx.Err() != nil {
			return
		}
		if !ok {
			status = "Barrido detenido: la emisora no confirmó el regreso a RX"
			return
		}
		if !captured {
			status = "Barrido detenido: cambió el estado de la emisora o se perdió la telemetría"
			return
		}
	}
}

func swrCapture(ctx context.Context, radio Radio, keyed time.Time, vfo int64, mode byte, point *Point) bool {
	limit, cancel := context.WithDeadline(ctx, keyed.Add(swrMaxTX))
	defer cancel()
	var rfAt, lastSWR time.Time
	var sawRX, sawTX, sawPowerSample, sawRF bool
	var values, powers [3]int
	n := 0
	stopped := false
	swrWait(limit, radio, func(s civ.State) bool {
		poAt := s.MeterAt[0x11]
		pow := s.Meters[0x11]
		if s.Frequency != vfo || s.Mode != mode || s.Split || s.Tuning.DeltaTX || (poAt.After(keyed) && pow > 115) {
			stopped = true
			return true
		}
		if s.TXKnown && s.TXAt.After(keyed) {
			sawTX = sawTX || s.TX
			sawRX = sawRX || !s.TX
		}
		powerFresh := s.TX && poAt.After(keyed) && time.Since(poAt) <= 300*time.Millisecond
		sawPowerSample = sawPowerSample || powerFresh
		sawRF = sawRF || powerFresh && pow > 0
		if !powerFresh || pow < 16 {
			n = 0
			rfAt = time.Time{}
			return false
		}
		if rfAt.IsZero() {
			rfAt = poAt
		}
		measured := s.MeterAt[0x12]
		if measured.Before(rfAt.Add(150*time.Millisecond)) || !measured.After(lastSWR) {
			return false
		}
		lastSWR = measured
		values[n%3], powers[n%3] = s.Meters[0x12], pow
		n++
		if n < 3 {
			return false
		}
		lo, hi := min(values[0], values[1], values[2]), max(values[0], values[1], values[2])
		if math.Abs(Ratio(hi)-Ratio(lo)) > .1 || hi > 120 && hi-lo > 4 || max(powers[0], powers[1], powers[2])-min(powers[0], powers[1], powers[2]) > 8 {
			return false
		}
		point.Raw, point.Power, point.Valid = values[0]+values[1]+values[2]-lo-hi, s.Meters[0x11], true
		return true
	})
	if point.Valid {
		return true
	}
	if stopped || ctx.Err() != nil || limit.Err() == nil {
		return false
	}
	point.Failure = NoReading
	if !sawRF && (sawPowerSample || sawRX && !sawTX) {
		point.Failure = NoTransmission
	}
	return true
}
