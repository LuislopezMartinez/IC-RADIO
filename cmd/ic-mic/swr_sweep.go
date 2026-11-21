package main

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/LuislopezMartinez/IC-RADIO/internal/civ"
	"github.com/LuislopezMartinez/IC-RADIO/internal/ptt"
)

const swrPowerSetting = 77 // nearest CI-V setting to 30% of the IC-7300's 100 W range
const swrToneHz = 1000
const swrMaxTX = 1500 * time.Millisecond

type swrPoint struct {
	Hz    int64 `json:"hz"`
	Raw   int   `json:"raw_swr"`
	Power int   `json:"raw_po"`
	Valid bool  `json:"valid"`
}

type swrReport struct {
	points []swrPoint
	status string
}

func swrFrequencies(center, step int64, count int) []swrPoint {
	points := make([]swrPoint, count)
	for i := range points {
		points[i].Hz = center + int64(i-count/2)*step
	}
	return points
}

func swrRatio(raw int) float64 {
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

func swrWait(ctx context.Context, radio *civ.Session, check func(civ.State) bool) (civ.State, bool) {
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

func swrWaitFor(ctx context.Context, radio *civ.Session, timeout time.Duration, check func(civ.State) bool) (civ.State, bool) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return swrWait(ctx, radio, check)
}

func runSWR(ctx context.Context, radio *civ.Session, keyer *ptt.Controller, tone *testToneProcessor, initial civ.State, points []swrPoint, out chan<- swrReport, done chan<- struct{}) {
	defer close(done)
	defer close(out)
	status := "Barrido completado"
	original := -1
	changedFrequency := false
	defer func() {
		tone.swrToneUntil.Store(0)
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
			if rx && original >= 0 {
				setAt := time.Now()
				rx = radio.SetPower(original)
				if rx {
					_, rx = swrWaitFor(cleanup, radio, 1200*time.Millisecond, func(s civ.State) bool { return s.PowerAt.After(setAt) && s.PowerSetting == original })
				}
			}
			if changedFrequency {
				at := time.Now()
				if radio.Tune(initial.Frequency) {
					_, tuned := swrWait(cleanup, radio, func(s civ.State) bool { return s.FrequencyAt.After(at) && s.Frequency == initial.Frequency })
					rx = rx && tuned
				} else {
					rx = false
				}
			}
			if !rx {
				status += " - comprueba frecuencia y potencia en el Icom"
			}
		}
		radio.MeasureSWR(false)
		tone.swrRunning.Store(false)
		if ctx.Err() != nil && status == "Barrido completado" {
			status = "Barrido detenido"
		}
		out <- swrReport{points: append([]swrPoint(nil), points...), status: status}
	}()

	tone.swrRunning.Store(true)
	radio.MeasureSWR(true)
	at := time.Now()
	if !radio.ReadPower() {
		status = "No se pudo consultar la potencia"
		return
	}
	state, ok := swrWaitFor(ctx, radio, 1500*time.Millisecond, func(s civ.State) bool { return s.PowerAt.After(at) && s.SplitAt.After(at) && s.TXKnown && !s.TX })
	if !ok || state.Split {
		status = "El test necesita RX con SPLIT desactivado"
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
		out <- swrReport{points: append([]swrPoint(nil), points...), status: fmt.Sprintf("Midiendo %d de %d", i+1, len(points))}
		vfo := point.Hz - swrToneHz
		if initial.Mode == 0 {
			vfo = point.Hz + swrToneHz
		}
		at = time.Now()
		if !radio.Tune(vfo) {
			status = "No se pudo solicitar la frecuencia de prueba"
			return
		}
		changedFrequency = true
		_, ok = swrWaitFor(ctx, radio, 1200*time.Millisecond, func(s civ.State) bool {
			return s.FrequencyAt.After(at) && s.Frequency == vfo && s.TXKnown && !s.TX && !s.Split && s.Mode == initial.Mode
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
		tone.swrToneUntil.Store(keyed.Add(swrMaxTX).UnixNano())
		swrCapture(ctx, radio, keyed, vfo, initial.Mode, point)
		tone.swrToneUntil.Store(0)
		releasedAt := time.Now()
		if err := keyer.SetTX(false); err != nil {
			status = "No se pudo liberar el PTT"
			keyer.Close()
			return
		}
		_, ok = swrWaitFor(ctx, radio, 1000*time.Millisecond, func(s civ.State) bool { return s.TXAt.After(releasedAt) && !s.TX })
		if !ok || !point.Valid {
			status = "Barrido detenido: sin lectura estable de ROE o potencia"
			return
		}
	}
}

func swrCapture(ctx context.Context, radio *civ.Session, keyed time.Time, vfo int64, mode byte, point *swrPoint) {
	limit, cancel := context.WithDeadline(ctx, keyed.Add(swrMaxTX))
	defer cancel()
	var rfAt, lastSWR time.Time
	var values, powers [3]int
	n := 0
	swrWait(limit, radio, func(s civ.State) bool {
		poAt := s.MeterAt[0x11]
		pow := s.Meters[0x11]
		if s.Frequency != vfo || s.Mode != mode || s.Split || (poAt.After(keyed) && pow > 115) {
			return true
		}
		if !s.TX || !poAt.After(keyed) || s.Meters[0x11] < 16 || time.Since(poAt) > 300*time.Millisecond {
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
		if math.Abs(swrRatio(hi)-swrRatio(lo)) > .1 || hi > 120 && hi-lo > 4 || max(powers[0], powers[1], powers[2])-min(powers[0], powers[1], powers[2]) > 8 {
			return false
		}
		point.Raw, point.Power, point.Valid = values[0]+values[1]+values[2]-lo-hi, s.Meters[0x11], true
		return true
	})
}
