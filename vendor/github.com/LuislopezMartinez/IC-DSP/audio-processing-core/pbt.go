package audioprocessing

import "math"

type Sideband string

const (
	USB Sideband = "USB"
	LSB Sideband = "LSB"
)

// TwinPBT isolates a selectable slice of a USB/LSB IQ signal and returns mono PCM.
// IQ input is interleaved I,Q,I,Q. The instance keeps streaming filter state.
type TwinPBT struct {
	inputRate, outputRate                   float64
	channelPhase, audioPhase, resamplePhase float64
	iFilter, qFilter                        [3]float64
	previousAudio, highpass                 float64
}

func NewTwinPBT(inputRate, outputRate float64) *TwinPBT {
	if outputRate <= 0 {
		outputRate = DefaultSampleRate
	}
	return &TwinPBT{inputRate: inputRate, outputRate: outputRate}
}

func (d *TwinPBT) Process(iq []float32, sideband Sideband, frequencyOffsetHz float64, lowCutHz, highCutHz int) []float32 {
	if sideband != USB && sideband != LSB {
		return nil
	}
	lowCutHz = min(max(lowCutHz, 50), 4800)
	highCutHz = min(max(highCutHz, lowCutHz+200), 5000)
	low, high := float64(lowCutHz), float64(highCutHz)
	sign := 1.0
	if sideband == LSB {
		sign = -1
	}
	center := (low + high) * .5
	half := (high - low) * .5
	channelStep := 2 * math.Pi * (frequencyOffsetHz + sign*center) / d.inputRate
	alpha := 1 - math.Exp(-2*math.Pi*math.Max(180, half*1.9)/d.inputRate)
	audioStep := 2 * math.Pi * sign * center / d.outputRate
	hpR := math.Exp(-2 * math.Pi * 120 / d.outputRate)
	out := make([]float32, 0, int(float64(len(iq)/2)*d.outputRate/d.inputRate)+1)
	for i := 0; i+1 < len(iq); i += 2 {
		c, s := math.Cos(d.channelPhase), math.Sin(d.channelPhase)
		si := float64(iq[i])*c + float64(iq[i+1])*s
		sq := float64(iq[i+1])*c - float64(iq[i])*s
		d.channelPhase = math.Remainder(d.channelPhase+channelStep, 2*math.Pi)
		d.iFilter[0] += alpha * (si - d.iFilter[0])
		d.qFilter[0] += alpha * (sq - d.qFilter[0])
		for stage := 1; stage < 3; stage++ {
			d.iFilter[stage] += alpha * (d.iFilter[stage-1] - d.iFilter[stage])
			d.qFilter[stage] += alpha * (d.qFilter[stage-1] - d.qFilter[stage])
		}
		d.resamplePhase += d.outputRate
		if d.resamplePhase < d.inputRate {
			continue
		}
		d.resamplePhase -= d.inputRate
		ac, as := math.Cos(d.audioPhase), math.Sin(d.audioPhase)
		audio := d.iFilter[2]*ac - d.qFilter[2]*as
		d.audioPhase = math.Remainder(d.audioPhase+audioStep, 2*math.Pi)
		d.highpass = audio - d.previousAudio + hpR*d.highpass
		d.previousAudio = audio
		out = append(out, float32(d.highpass))
	}
	return out
}
