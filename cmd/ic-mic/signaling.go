package main

import (
	"fmt"
	"math"
	"strings"
	"sync"
	"sync/atomic"

	audio "github.com/LuislopezMartinez/IC-DSP/audio-processing-core"
	rl "github.com/gen2brain/raylib-go/raylib"
)

const (
	signalTone  = "tone"
	signalPause = "pause"
	signalSweep = "sweep"
	signalMorse = "morse"
)

type signalBlock struct {
	Type       string  `json:"type"`
	Note       int     `json:"note,omitempty"`
	DurationMS float64 `json:"duration_ms,omitempty"`
	Style      string  `json:"style,omitempty"`
	Callsign   string  `json:"callsign,omitempty"`
	WPM        int     `json:"wpm,omitempty"`
}

type signalingSettings struct {
	StartEnabled bool          `json:"start_enabled"`
	EndEnabled   bool          `json:"end_enabled"`
	RXCueVolume  float64       `json:"rx_cue_volume"`
	Start        []signalBlock `json:"start"`
	End          []signalBlock `json:"end"`
}

type signalSegment struct {
	frequency, endFrequency float64
	samples                 int64
	tone                    bool
	waveform                string
}

type signalSequencer struct {
	mu       sync.Mutex
	segments []signalSegment
	index    int
	position int64
	phase    float64
	active   atomic.Bool
	done     atomic.Bool
	cueGain  atomic.Uint64
}

func newSignalSequencer() *signalSequencer {
	s := &signalSequencer{}
	s.SetCueVolume(.18)
	return s
}

func (s *signalSequencer) SetCueVolume(value float64) {
	value = max(.02, min(.55, value))
	s.cueGain.Store(math.Float64bits(value))
}

func (s *signalSequencer) CueVolume() float64 { return math.Float64frombits(s.cueGain.Load()) }
func (s *signalSequencer) Active() bool       { return s != nil && s.active.Load() }
func (s *signalSequencer) Done() bool         { return s != nil && s.done.Swap(false) }

func (s *signalSequencer) Start(blocks []signalBlock) bool {
	segments := expandSignalBlocks(blocks)
	if len(segments) == 0 {
		return false
	}
	s.mu.Lock()
	s.segments, s.index, s.position, s.phase = segments, 0, 0, 0
	s.done.Store(false)
	s.active.Store(true)
	s.mu.Unlock()
	return true
}

func (s *signalSequencer) Stop() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.segments, s.index, s.position, s.phase = nil, 0, 0, 0
	s.active.Store(false)
	s.done.Store(false)
	s.mu.Unlock()
}

// Render replaces microphone samples while a signaling sequence is active.
// It returns true when at least part of this buffer belongs to the sequence.
func (s *signalSequencer) Render(samples []float32) bool {
	if s == nil || !s.active.Load() {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.active.Load() {
		return false
	}
	used := false
	for i := range samples {
		if s.index >= len(s.segments) {
			s.active.Store(false)
			s.done.Store(true)
			break
		}
		used = true
		segment := s.segments[s.index]
		if segment.tone {
			progress := float64(s.position) / float64(max(int64(1), segment.samples-1))
			frequency := segment.frequency + (segment.endFrequency-segment.frequency)*progress
			value := math.Sin(s.phase)
			switch segment.waveform {
			case "square":
				if value >= 0 {
					value = 1
				} else {
					value = -1
				}
			case "triangle":
				value = 2 / math.Pi * math.Asin(value)
			}
			// A short automatic envelope is deliberately hidden from the UI. It
			// prevents hard waveform edges (especially square waves) from becoming
			// loud clicks at the start and end of every note.
			fadeSamples := min(int64(audio.DefaultSampleRate/200), max(int64(1), segment.samples/2))
			remaining := segment.samples - s.position - 1
			envelope := min(1.0, min(float64(s.position)/float64(fadeSamples), float64(max(int64(0), remaining))/float64(fadeSamples)))
			samples[i] = float32(.22 * value * envelope)
			s.phase += 2 * math.Pi * frequency / audio.DefaultSampleRate
			if s.phase >= 2*math.Pi {
				s.phase -= 2 * math.Pi
			}
		} else {
			samples[i] = 0
		}
		s.position++
		if s.position >= segment.samples {
			s.index++
			s.position = 0
		}
	}
	return used
}

func expandSignalBlocks(blocks []signalBlock) []signalSegment {
	segments := make([]signalSegment, 0, len(blocks)*3)
	appendMS := func(frequency, end, milliseconds float64, tone bool, waveform string) {
		if milliseconds <= 0 {
			return
		}
		segments = append(segments, signalSegment{frequency: frequency, endFrequency: end, samples: int64(milliseconds * audio.DefaultSampleRate / 1000), tone: tone, waveform: waveform})
	}
	for _, block := range blocks {
		duration := block.DurationMS
		if duration <= 0 {
			duration = 160
		}
		switch block.Type {
		case signalTone:
			frequency := midiFrequency(block.Note)
			appendMS(frequency, frequency, duration, true, block.Style)
		case signalPause:
			appendMS(0, 0, duration, false, "")
		case signalSweep:
			frequency := midiFrequency(block.Note)
			appendMS(frequency, frequency*1.5, duration, true, block.Style)
		case signalMorse:
			wpm := block.WPM
			if wpm <= 0 {
				wpm = 18
			}
			dot := 1200 / float64(wpm)
			frequency := midiFrequency(block.Note)
			for ci, char := range strings.ToUpper(block.Callsign) {
				pattern := morsePatterns[char]
				if pattern == "" {
					continue
				}
				if ci > 0 {
					appendMS(0, 0, dot*3, false, "")
				}
				for si, symbol := range pattern {
					if si > 0 {
						appendMS(0, 0, dot, false, "")
					}
					length := dot
					if symbol == '-' {
						length *= 3
					}
					appendMS(frequency, frequency, length, true, block.Style)
				}
			}
		}
	}
	return segments
}

var morsePatterns = map[rune]string{
	'A': ".-", 'B': "-...", 'C': "-.-.", 'D': "-..", 'E': ".", 'F': "..-.", 'G': "--.", 'H': "....", 'I': "..", 'J': ".---", 'K': "-.-", 'L': ".-..", 'M': "--", 'N': "-.", 'O': "---", 'P': ".--.", 'Q': "--.-", 'R': ".-.", 'S': "...", 'T': "-", 'U': "..-", 'V': "...-", 'W': ".--", 'X': "-..-", 'Y': "-.--", 'Z': "--..",
	'0': "-----", '1': ".----", '2': "..---", '3': "...--", '4': "....-", '5': ".....", '6': "-....", '7': "--...", '8': "---..", '9': "----.",
}

func midiFrequency(note int) float64 {
	if note < 36 || note > 95 {
		note = 60
	}
	return 440 * math.Pow(2, float64(note-69)/12)
}

var noteNames = []string{"DO", "DO#", "RE", "RE#", "MI", "FA", "FA#", "SOL", "SOL#", "LA", "LA#", "SI"}

func noteLabel(note int) string {
	if note < 0 {
		note = 60
	}
	return fmt.Sprintf("%s%d", noteNames[note%12], note/12-1)
}

func defaultSignalingSettings() signalingSettings {
	return signalingSettings{StartEnabled: true, EndEnabled: true, RXCueVolume: .18,
		Start: []signalBlock{{Type: signalTone, Note: 60, DurationMS: 140, Style: "sine"}, {Type: signalPause, DurationMS: 70}, {Type: signalTone, Note: 67, DurationMS: 180, Style: "sine"}},
		End:   []signalBlock{{Type: signalTone, Note: 72, DurationMS: 110, Style: "sine"}, {Type: signalTone, Note: 67, DurationMS: 170, Style: "sine"}},
	}
}

func (a *app) activeSignalBlocks() *[]signalBlock {
	if a.signalTab == "end" {
		return &a.settings.Signaling.End
	}
	return &a.settings.Signaling.Start
}

func (a *app) previewSignal(blocks []signalBlock) {
	if len(blocks) == 0 || a.signalSequencer == nil {
		return
	}
	a.ensureSignalMonitor()
	a.signalSequencer.SetCueVolume(a.settings.Signaling.RXCueVolume)
	a.signalSequencer.Start(blocks)
	a.signalPreview = true
	a.message = "Preview playing through RX output"
}

func (a *app) ensureSignalMonitor() {
	if a.monitor == nil || a.settings.MonitorEnabled {
		return
	}
	if err := a.startOutputMonitor(); err == nil {
		a.signalMonitorTemporary = true
	} else {
		a.message = "RX preview unavailable: " + err.Error()
	}
}

func (a *app) stopTemporarySignalMonitor() {
	if a.signalMonitorTemporary && a.monitor != nil && !a.settings.MonitorEnabled {
		a.monitor.Stop()
	}
	a.signalMonitorTemporary = false
}

func signalPaletteRect(index int) rl.Rectangle {
	return rl.NewRectangle(48+float32(index)*150, 680, 138, 48)
}
func signalTimelineRect() rl.Rectangle  { return rl.NewRectangle(45, 475, 1070, 182) }
func signalInspectorRect() rl.Rectangle { return rl.NewRectangle(1135, 455, 420, 390) }

func signalBlockRect(index, count int) rl.Rectangle {
	width, gap := signalBlockSize(count)
	total := float32(count)*width + float32(max(0, count-1))*gap
	start := float32(175)
	if total < 805 {
		start += (805 - total) / 2
	}
	return rl.NewRectangle(start+float32(index)*(width+gap), 520, width, 102)
}

func signalBlockSize(count int) (float32, float32) {
	gap := float32(10)
	if count <= 0 {
		return 132, gap
	}
	return min(float32(132), (805-float32(count-1)*gap)/float32(count)), gap
}

func (a *app) updateSignalEditor(mouse rl.Vector2) {
	if clicked(mouse, rl.NewRectangle(340, 397, 315, 52)) {
		a.signalTab, a.signalSelected = "start", -1
	}
	if clicked(mouse, rl.NewRectangle(665, 397, 315, 52)) {
		a.signalTab, a.signalSelected = "end", -1
	}
	if clicked(mouse, rl.NewRectangle(990, 397, 125, 52)) {
		if a.signalTab == "end" {
			a.settings.Signaling.EndEnabled = !a.settings.Signaling.EndEnabled
		} else {
			a.settings.Signaling.StartEnabled = !a.settings.Signaling.StartEnabled
		}
		a.save()
	}
	blocks := a.activeSignalBlocks()
	if rl.IsKeyPressed(rl.KeySpace) && !a.signalEditingCallsign {
		a.previewSignal(append([]signalBlock(nil), (*blocks)...))
	}
	palette := []string{signalTone, signalPause, signalSweep, signalMorse}
	for i, kind := range palette {
		r := signalPaletteRect(i)
		if rl.IsMouseButtonPressed(rl.MouseLeftButton) && rl.CheckCollisionPointRec(mouse, r) {
			a.signalPaletteDrag = kind
		}
	}
	if a.signalPaletteDrag != "" && rl.IsMouseButtonReleased(rl.MouseLeftButton) {
		if rl.CheckCollisionPointRec(mouse, signalTimelineRect()) && len(*blocks) < 8 {
			block := signalBlock{Type: a.signalPaletteDrag, Note: 60, DurationMS: 160, Style: "sine", Callsign: "EA3IWW", WPM: 18}
			if block.Type == signalPause {
				block.DurationMS = 100
			}
			*blocks, a.signalSelected = insertSignalBlock(*blocks, block, a.signalSelected)
			a.save()
		}
		a.signalPaletteDrag = ""
	}
	for i := range *blocks {
		r := signalBlockRect(i, len(*blocks))
		if rl.IsMouseButtonPressed(rl.MouseLeftButton) && rl.CheckCollisionPointRec(mouse, r) {
			a.signalSelected, a.signalDrag = i, i
		}
	}
	if a.signalDrag >= 0 && rl.IsMouseButtonReleased(rl.MouseLeftButton) {
		if rl.CheckCollisionPointRec(mouse, signalTimelineRect()) && len(*blocks) > 1 {
			width, gap := signalBlockSize(len(*blocks))
			target := int((mouse.X - signalBlockRect(0, len(*blocks)).X + width/2) / (width + gap))
			target = max(0, min(len(*blocks)-1, target))
			if target != a.signalDrag {
				item := (*blocks)[a.signalDrag]
				copy((*blocks)[a.signalDrag:], (*blocks)[a.signalDrag+1:])
				*blocks = (*blocks)[:len(*blocks)-1]
				*blocks = append((*blocks), signalBlock{})
				copy((*blocks)[target+1:], (*blocks)[target:])
				(*blocks)[target] = item
				a.signalSelected = target
			}
		}
		a.signalDrag = -1
		a.save()
	}
	if clicked(mouse, rl.NewRectangle(48, 802, 220, 42)) {
		a.previewSignal(append([]signalBlock(nil), (*blocks)...))
	}
	if clicked(mouse, rl.NewRectangle(278, 802, 165, 42)) && a.signalSelected >= 0 && a.signalSelected < len(*blocks) {
		*blocks = append((*blocks)[:a.signalSelected], (*blocks)[a.signalSelected+1:]...)
		a.signalSelected = min(a.signalSelected, len(*blocks)-1)
		a.save()
	}
	if slider := rl.NewRectangle(470, 829, 620, 14); rl.IsMouseButtonDown(rl.MouseLeftButton) && rl.CheckCollisionPointRec(mouse, rl.NewRectangle(slider.X, slider.Y-14, slider.Width, 36)) {
		a.settings.Signaling.RXCueVolume = .04 + float64((mouse.X-slider.X)/slider.Width)*.36
		a.signalSequencer.SetCueVolume(a.settings.Signaling.RXCueVolume)
	}
	if a.signalSelected < 0 || a.signalSelected >= len(*blocks) {
		return
	}
	block := &(*blocks)[a.signalSelected]
	if block.Type == signalMorse && clicked(mouse, rl.NewRectangle(1158, 724, 374, 40)) {
		a.signalEditingCallsign = true
	}
	if clicked(mouse, rl.NewRectangle(1158, 801, 374, 36)) {
		a.previewSignal([]signalBlock{*block})
	}
	if block.Type != signalMorse {
		a.signalEditingCallsign = false
	}
	if (block.Type == signalTone || block.Type == signalSweep || block.Type == signalMorse) && !a.signalEditingCallsign {
		a.updateSignalPiano(mouse, block)
	}
	if block.Type == signalTone || block.Type == signalSweep || block.Type == signalMorse {
		for i, waveform := range []string{"sine", "square", "triangle"} {
			if clicked(mouse, rl.NewRectangle(1163+float32(i)*122, 678, 112, 32)) {
				block.Style = waveform
				a.previewSignal([]signalBlock{*block})
				a.save()
			}
		}
	}
	if rl.IsKeyPressed(rl.KeyEnter) && !a.signalEditingCallsign && block.Type != signalMorse {
		if a.signalSelected+1 < len(*blocks) {
			a.signalSelected++
		} else if len(*blocks) < 8 {
			*blocks = append(*blocks, signalBlock{Type: signalTone, Note: block.Note, DurationMS: 170, Style: "sine"})
			a.signalSelected = len(*blocks) - 1
		}
		a.save()
	}
	for i, duration := range []float64{90, 170, 320} {
		if clicked(mouse, rl.NewRectangle(1158+float32(i)*126, 730, 116, 34)) {
			block.DurationMS = duration
			a.save()
		}
	}
	if block.Type == signalMorse && a.signalEditingCallsign {
		for key := rl.GetCharPressed(); key > 0; key = rl.GetCharPressed() {
			if (key >= 'A' && key <= 'Z') || (key >= 'a' && key <= 'z') || (key >= '0' && key <= '9') {
				if len(block.Callsign) < 10 {
					block.Callsign += strings.ToUpper(string(key))
				}
			}
		}
		if rl.IsKeyPressed(rl.KeyBackspace) && len(block.Callsign) > 0 {
			block.Callsign = block.Callsign[:len(block.Callsign)-1]
		}
		if rl.IsKeyPressed(rl.KeyEnter) {
			a.signalEditingCallsign = false
			a.save()
		}
	}
}

func insertSignalBlock(blocks []signalBlock, block signalBlock, selected int) ([]signalBlock, int) {
	insertAt := len(blocks)
	if selected >= 0 && selected < len(blocks) {
		insertAt = selected + 1
	}
	blocks = append(blocks, signalBlock{})
	copy(blocks[insertAt+1:], blocks[insertAt:])
	blocks[insertAt] = block
	return blocks, insertAt
}

type pianoKeyBinding struct {
	key      int32
	semitone int
	label    string
}

var pianoBindings = []pianoKeyBinding{
	{rl.KeyA, 0, "A"}, {rl.KeyW, 1, "W"}, {rl.KeyS, 2, "S"}, {rl.KeyE, 3, "E"}, {rl.KeyD, 4, "D"}, {rl.KeyF, 5, "F"}, {rl.KeyT, 6, "T"}, {rl.KeyG, 7, "G"}, {rl.KeyY, 8, "Y"}, {rl.KeyH, 9, "H"}, {rl.KeyU, 10, "U"}, {rl.KeyJ, 11, "J"},
	{rl.KeyZ, 12, "Z"}, {rl.KeyTwo, 13, "2"}, {rl.KeyX, 14, "X"}, {rl.KeyThree, 15, "3"}, {rl.KeyC, 16, "C"}, {rl.KeyV, 17, "V"}, {rl.KeyFive, 18, "5"}, {rl.KeyB, 19, "B"}, {rl.KeySix, 20, "6"}, {rl.KeyN, 21, "N"}, {rl.KeySeven, 22, "7"}, {rl.KeyM, 23, "M"},
}

func (a *app) updateSignalPiano(mouse rl.Vector2, block *signalBlock) {
	if a.signalPianoBase == 0 {
		a.signalPianoBase = 48
	}
	if rl.IsKeyPressed(rl.KeyPageUp) || clicked(mouse, signalOctaveUpRect()) {
		a.signalPianoBase = min(72, a.signalPianoBase+12)
	}
	if rl.IsKeyPressed(rl.KeyPageDown) || clicked(mouse, signalOctaveDownRect()) {
		a.signalPianoBase = max(36, a.signalPianoBase-12)
	}
	for _, binding := range pianoBindings {
		if rl.IsKeyPressed(binding.key) {
			block.Note = a.signalPianoBase + binding.semitone
			a.previewSignal([]signalBlock{pianoPreviewBlock(*block)})
			a.save()
		}
	}
	for white := 0; white < 14; white++ {
		if clicked(mouse, signalPianoWhiteRect(white)) {
			semitones := []int{0, 2, 4, 5, 7, 9, 11}
			block.Note = a.signalPianoBase + (white/7)*12 + semitones[white%7]
			a.previewSignal([]signalBlock{pianoPreviewBlock(*block)})
			a.save()
		}
	}
	for _, black := range signalPianoBlackKeys() {
		if clicked(mouse, black.rect) {
			block.Note = a.signalPianoBase + black.offset
			a.previewSignal([]signalBlock{pianoPreviewBlock(*block)})
			a.save()
		}
	}
}

func pianoPreviewBlock(block signalBlock) signalBlock {
	style := block.Style
	if style == "" || style == "clean" {
		style = "sine"
	}
	return signalBlock{Type: signalTone, Note: block.Note, DurationMS: 240, Style: style}
}

func signalOctaveDownRect() rl.Rectangle { return rl.NewRectangle(1390, 517, 68, 28) }
func signalOctaveUpRect() rl.Rectangle   { return rl.NewRectangle(1464, 517, 68, 28) }

func drawOctaveButton(r rl.Rectangle, up bool) {
	drawButton(r, "OCT", false)
	cx, cy := r.X+13, r.Y+r.Height/2
	if up {
		rl.DrawTriangle(rl.NewVector2(cx, cy-5), rl.NewVector2(cx-6, cy+5), rl.NewVector2(cx+6, cy+5), color.amber)
	} else {
		rl.DrawTriangle(rl.NewVector2(cx, cy+5), rl.NewVector2(cx+6, cy-5), rl.NewVector2(cx-6, cy-5), color.amber)
	}
}

func signalPianoWhiteRect(index int) rl.Rectangle {
	return rl.NewRectangle(1158+float32(index)*26.5, 550, 25.5, 100)
}

type signalBlackKey struct {
	offset int
	label  string
	rect   rl.Rectangle
}

func signalPianoBlackKeys() []signalBlackKey {
	result := make([]signalBlackKey, 0, 10)
	for octave := 0; octave < 2; octave++ {
		baseX := float32(1158) + float32(octave*7)*26.5
		keys := []struct {
			offset int
			x      float32
			label  string
		}{{1, 18, "W"}, {3, 44.5, "E"}, {6, 97.5, "T"}, {8, 124, "Y"}, {10, 150.5, "U"}}
		if octave == 1 {
			keys = []struct {
				offset int
				x      float32
				label  string
			}{{1, 18, "2"}, {3, 44.5, "3"}, {6, 97.5, "5"}, {8, 124, "6"}, {10, 150.5, "7"}}
		}
		for _, key := range keys {
			result = append(result, signalBlackKey{offset: octave*12 + key.offset, label: key.label, rect: rl.NewRectangle(baseX+key.x, 550, 17, 62)})
		}
	}
	return result
}

func (a *app) drawSignalEditor(mouse rl.Vector2) {
	panel := rl.NewRectangle(30, 385, 1540, 475)
	drawInsetPanel(panel, .025)
	drawMilitaryText("SONIC SEQUENCER", 50, 402, 28, color.text)
	startTab := rl.NewRectangle(340, 397, 315, 52)
	endTab := rl.NewRectangle(665, 397, 315, 52)
	drawButton(startTab, "AL PULSAR PTT  ·  APERTURA", a.signalTab != "end")
	drawButton(endTab, "AL SOLTAR PTT  ·  CIERRE", a.signalTab == "end")
	enabled := a.settings.Signaling.StartEnabled
	if a.signalTab == "end" {
		enabled = a.settings.Signaling.EndEnabled
	}
	drawButton(rl.NewRectangle(990, 397, 125, 52), "ACTIVO", enabled)
	sequenceHelp := "PRIMERO SUENA ESTA MELODÍA · DESPUÉS SE ABRE EL MICRÓFONO"
	if a.signalTab == "end" {
		sequenceHelp = "PRIMERO SE CIERRA EL MICRÓFONO · DESPUÉS SUENA ESTA MELODÍA"
	}
	drawText(sequenceHelp, 55, 454, 14, color.amber)
	track := signalTimelineRect()
	rl.DrawRectangleRounded(track, .08, 10, rl.NewColor(10, 22, 29, 255))
	rl.DrawRectangleRoundedLines(track, .08, 10, rl.NewColor(55, 78, 88, 255))
	blocks := *a.activeSignalBlocks()
	for i, block := range blocks {
		a.drawSignalBlock(signalBlockRect(i, len(blocks)), block, i == a.signalSelected)
	}
	drawButton(rl.NewRectangle(60, 520, 100, 102), func() string {
		if a.signalTab == "end" {
			return "MIC OFF"
		}
		return "PTT ON"
	}(), false)
	drawButton(rl.NewRectangle(1020, 520, 80, 102), func() string {
		if a.signalTab == "end" {
			return "RX"
		}
		return "HABLA"
	}(), true)
	if len(blocks) == 0 {
		drawCentered("SUELTA AQUÍ TU PRIMER SONIDO", track, 21, color.muted, 0)
	}
	drawText("PIEZAS - ARRASTRA A LA PISTA", 48, 660, 13, color.muted)
	labels := []struct{ kind, label string }{{signalTone, "TONO"}, {signalPause, "PAUSA"}, {signalSweep, "BARRIDO"}, {signalMorse, "MORSE"}}
	for i, item := range labels {
		r := signalPaletteRect(i)
		fill := []rl.Color{color.cyan, rl.NewColor(240, 130, 165, 255), rl.NewColor(178, 112, 238, 255), color.amber}[i]
		rl.DrawRectangleRounded(r, .22, 8, fill)
		drawCentered(item.label, r, 17, rl.NewColor(12, 20, 24, 255), 0)
	}
	drawButton(rl.NewRectangle(48, 802, 220, 42), "PROBAR EN RX", false)
	drawButton(rl.NewRectangle(278, 802, 165, 42), "ELIMINAR", false)
	drawText("VOLUMEN DEL AVISO RX", 470, 797, 13, color.text)
	drawMechanicalSlider(rl.NewRectangle(470, 829, 620, 14), (a.settings.Signaling.RXCueVolume-.04)/.36*100, controlColor.active)
	drawText("SUAVE", 470, 812, 12, color.muted)
	drawText("CLARO", 1045, 812, 12, color.muted)
	a.drawSignalInspector(mouse, blocks)
	if a.signalPaletteDrag != "" {
		r := rl.NewRectangle(mouse.X-60, mouse.Y-25, 120, 50)
		rl.DrawRectangleRounded(r, .2, 8, rl.NewColor(255, 190, 40, 225))
		drawCentered(strings.ToUpper(a.signalPaletteDrag), r, 16, dspColor.text, 0)
	}
}

func (a *app) drawSignalBlock(r rl.Rectangle, block signalBlock, selected bool) {
	fill := color.cyan
	typeLabel, label := "TONO", noteLabel(block.Note)
	switch block.Type {
	case signalPause:
		fill, typeLabel, label = rl.NewColor(236, 130, 163, 255), "PAUSA", durationLabel(block.DurationMS)
	case signalSweep:
		fill, typeLabel, label = rl.NewColor(178, 112, 238, 255), "BARRIDO", noteLabel(block.Note)
	case signalMorse:
		fill, typeLabel, label = color.amber, "MORSE", block.Callsign
	}
	if selected {
		for i := 5; i > 0; i-- {
			e := float32(i * 2)
			rl.DrawRectangleRounded(rl.NewRectangle(r.X-e, r.Y-e, r.Width+2*e, r.Height+2*e), .18, 8, rl.NewColor(color.cyan.R, color.cyan.G, color.cyan.B, uint8(12+i*5)))
		}
	}
	rl.DrawRectangleRounded(r, .18, 8, fill)
	rl.DrawRectangleRoundedLines(r, .18, 8, rl.NewColor(235, 247, 248, 210))
	drawCentered(typeLabel, r, 13, rl.NewColor(10, 24, 29, 255), -21)
	drawCentered(label, r, 17, rl.NewColor(10, 24, 29, 255), 13)
}

func durationLabel(ms float64) string {
	if ms <= 100 {
		return "CORTA"
	}
	if ms >= 260 {
		return "LARGA"
	}
	return "NORMAL"
}

func (a *app) drawSignalInspector(mouse rl.Vector2, blocks []signalBlock) {
	r := signalInspectorRect()
	rl.DrawRectangleRounded(r, .04, 8, rl.NewColor(225, 229, 227, 255))
	rl.DrawRectangleRoundedLines(r, .04, 8, dspColor.border)
	drawText("EDITAR BLOQUE", 1158, 470, 23, dspColor.text)
	if a.signalSelected < 0 || a.signalSelected >= len(blocks) {
		drawWrappedText("Selecciona un bloque para cambiar cómo suena.", 1158, 520, 374, 17, 26, dspColor.muted)
		return
	}
	block := blocks[a.signalSelected]
	heading := strings.ToUpper(block.Type) + " · " + noteLabel(block.Note)
	if block.Type == signalMorse {
		heading = "MORSE · " + block.Callsign
	}
	drawText(heading, 1158, 504, 17, dspColor.amber)
	if block.Type == signalTone || block.Type == signalSweep || block.Type == signalMorse {
		drawText(fmt.Sprintf("PIANO %s-%s", noteLabel(a.signalPianoBase), noteLabel(a.signalPianoBase+23)), 1158, 525, 12, dspColor.muted)
		drawOctaveButton(signalOctaveDownRect(), false)
		drawOctaveButton(signalOctaveUpRect(), true)
		whiteNotes := []string{"DO", "RE", "MI", "FA", "SOL", "LA", "SI"}
		whiteOffsets := []int{0, 2, 4, 5, 7, 9, 11}
		for white := 0; white < 14; white++ {
			key := signalPianoWhiteRect(white)
			selected := block.Note == a.signalPianoBase+(white/7)*12+whiteOffsets[white%7]
			fill := rl.NewColor(248, 248, 241, 255)
			if selected {
				fill = color.amber
			}
			rl.DrawRectangleRounded(key, .04, 3, fill)
			rl.DrawRectangleRoundedLines(key, .04, 3, dspColor.border)
			drawCentered(whiteNotes[white%7], key, 8, dspColor.text, 35)
		}
		for _, black := range signalPianoBlackKeys() {
			selected := block.Note == a.signalPianoBase+black.offset
			fill := rl.NewColor(19, 28, 33, 255)
			if selected {
				fill = color.amber
			}
			rl.DrawRectangleRounded(black.rect, .05, 3, fill)
			drawCentered(black.label, black.rect, 8, color.text, 18)
		}
	}
	if block.Type == signalMorse {
		field := rl.NewRectangle(1158, 724, 374, 40)
		fill := rl.NewColor(236, 239, 237, 255)
		if a.signalEditingCallsign {
			fill = rl.NewColor(255, 226, 150, 255)
		}
		rl.DrawRectangleRounded(field, .1, 5, fill)
		rl.DrawRectangleRoundedLines(field, .1, 5, dspColor.border)
		drawText("INDICATIVO: "+block.Callsign, 1172, 734, 17, dspColor.text)
		drawText("Clic para escribir · Enter confirma · 18 WPM", 1158, 768, 11, dspColor.muted)
	} else {
		drawText("DURACIÓN", 1158, 716, 11, dspColor.muted)
		for i, label := range []string{"CORTO", "NORMAL", "LARGO"} {
			drawButton(rl.NewRectangle(1158+float32(i)*126, 730, 116, 34), label, (i == 0 && block.DurationMS <= 100) || (i == 1 && block.DurationMS > 100 && block.DurationMS < 260) || (i == 2 && block.DurationMS >= 260))
		}
	}
	if block.Type == signalTone || block.Type == signalSweep || block.Type == signalMorse {
		style := block.Style
		if style == "" || style == "clean" {
			style = "sine"
		}
		group := rl.NewRectangle(1153, 654, 384, 62)
		rl.DrawRectangleRounded(group, .06, 5, rl.NewColor(202, 207, 205, 255))
		rl.DrawRectangleRoundedLines(group, .06, 5, dspColor.border)
		drawText("TIPO DE TONO", 1163, 659, 11, dspColor.muted)
		for i, item := range []struct{ key, label string }{{"sine", "SENOIDAL"}, {"square", "CUADRADA"}, {"triangle", "TRIANGULAR"}} {
			drawButton(rl.NewRectangle(1163+float32(i)*122, 678, 112, 32), item.label, style == item.key)
		}
	}
	if block.Type == signalMorse {
		drawText("TECLADO PC: A-J / Z-M", 1158, 785, 9, dspColor.muted)
	} else {
		drawText("TECLADO PC: A-J Y Z-M · TOCA PARA ELEGIR", 1158, 773, 12, dspColor.muted)
	}
	drawButton(rl.NewRectangle(1158, 801, 374, 36), "ESCUCHAR BLOQUE", false)
}
