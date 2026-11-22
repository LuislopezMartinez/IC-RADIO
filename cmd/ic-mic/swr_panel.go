package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/LuislopezMartinez/IC-RADIO/internal/civ"
	"github.com/LuislopezMartinez/IC-RADIO/internal/swr"
	rl "github.com/gen2brain/raylib-go/raylib"
)

type swrPanel struct {
	open, show  bool
	history     map[byte]swrSavedSweep
	step, count int
	status      string
	resultStep  int64
	points      []swr.Point
	cancel      context.CancelFunc
	reports     <-chan swr.Report
	done        <-chan struct{}
}

func swrOverlayButton() rl.Rectangle { return rl.NewRectangle(1224, 238, 46, 19) }
func swrTestButton() rl.Rectangle    { return rl.NewRectangle(1224, 259, 46, 19) }
func swrPlayButton() rl.Rectangle    { return rl.NewRectangle(304, 597, 179, 49) }
func swrCloseButton() rl.Rectangle   { return rl.NewRectangle(1254, 429, 40, 32) }
func swrStepButton() rl.Rectangle    { return rl.NewRectangle(1121, 496, 169, 53) }
func swrCountButton() rl.Rectangle   { return rl.NewRectangle(1121, 561, 169, 53) }

const swrThreshold = 1.5

var swrCool = rl.NewColor(33, 192, 234, 255)
var swrWarm = rl.NewColor(255, 140, 52, 255)

func (panel *swrPanel) close() {
	if panel.cancel != nil {
		panel.cancel()
		<-panel.done
	}
}

type swrSavedSweep struct {
	Step       int64       `json:"step_hz"`
	MeasuredAt time.Time   `json:"measured_at"`
	Points     []swr.Point `json:"points"`
}

func swrBandCode(frequency int64) byte {
	if band := bandAtFrequency(frequency); band >= 0 {
		return radioBands[band].code
	}
	return 0
}

func completeSWRSweep(points []swr.Point) bool {
	if len(points) < 3 || len(points) > 13 || len(points)%2 == 0 {
		return false
	}
	for _, point := range points {
		if !point.Valid && point.Failure != swr.NoTransmission && point.Failure != swr.NoReading {
			return false
		}
	}
	return true
}

func (a *app) consumeSWRReports() {
	p := &a.radio.swr
	for p.reports != nil {
		select {
		case report, ok := <-p.reports:
			if ok {
				if report.Points != nil {
					p.points = report.Points
				}
				p.status = report.Status
				continue
			}
			p.reports, p.cancel = nil, nil
			if completeSWRSweep(p.points) {
				if p.history == nil {
					p.history = make(map[byte]swrSavedSweep)
				}
				band := swrBandCode(p.points[len(p.points)/2].Hz)
				p.history[band] = swrSavedSweep{Step: p.resultStep, MeasuredAt: time.Now(), Points: p.points}
				a.settings.SWRHistory = p.history
				a.save()
			}
		default:
			return
		}
	}
}

func (a *app) updateSWR(mouse rl.Vector2) bool {
	a.consumeSWRReports()
	p := &a.radio.swr
	if p.cancel != nil {
		if clicked(mouse, swrPlayButton()) || rl.IsKeyPressed(rl.KeyEscape) || !rl.IsWindowFocused() {
			p.cancel()
		}
		return true
	}
	if p.open {
		if clicked(mouse, swrTestButton()) || rl.IsKeyPressed(rl.KeyEscape) || clicked(mouse, swrCloseButton()) {
			p.open = false
		} else if clicked(mouse, swrPlayButton()) {
			a.startSWR()
		} else if clicked(mouse, swrStepButton()) {
			p.step = (p.step + 1) % len(swr.Steps)
		} else if clicked(mouse, swrCountButton()) {
			p.count = (p.count + 1) % 6
		}
		return true
	}
	if a.radio.page != pageOperation || a.radio.editing || a.radio.modeMenu || a.radio.bands.open ||
		!a.radio.bands.requestAt.IsZero() || a.pttSettingsOpen || a.transmitting() {
		return false
	}
	if clicked(mouse, swrOverlayButton()) {
		p.show = !p.show
		a.settings.SWROverlay = p.show
		a.save()
		return true
	}
	if clicked(mouse, swrTestButton()) {
		state, _ := a.radio.snapshot()
		p.openSavedSweep(state.Frequency)
		p.open = true
		return true
	}
	return false
}

func (p *swrPanel) openSavedSweep(center int64) {
	saved, ok := p.history[swrBandCode(center)]
	if center <= 0 {
		for _, candidate := range p.history {
			if candidate.MeasuredAt.After(saved.MeasuredAt) {
				saved, ok = candidate, true
			}
		}
	}
	if !ok || !completeSWRSweep(saved.Points) {
		p.points, p.resultStep, p.status = nil, 0, ""
		return
	}
	for i, step := range swr.Steps {
		if step == saved.Step {
			p.step, p.count = i, (13-len(saved.Points))/2
			p.resultStep, p.points = saved.Step, saved.Points
			p.status = "Medición guardada · " + saved.MeasuredAt.Local().Format("02/01/2006 15:04")
			return
		}
	}
}

func (a *app) startSWR() {
	p := &a.radio.swr
	state, online := a.radio.snapshot()
	if !online || state.PowerPending || !state.TXKnown || state.TX || a.transmitting() || state.Frequency <= 0 {
		p.status = "Conecta el Icom y deja la emisora en RX"
		return
	}
	if !state.ModeKnown || state.Mode > 1 {
		p.status = "Selecciona USB o LSB con modulacion de audio USB"
		return
	}
	client := a.radio.remote
	session, local := a.radio.session.(*civ.Session)
	if client == nil && (!local || a.txView.engine == nil || !a.txView.engine.Status().Running ||
		!isRadioUSBCodec(deviceName(a.txView.outputs, a.txView.outIndex))) {
		p.status = "Selecciona USB Audio CODEC como salida en Audio TX"
		return
	}
	request := swr.Request{Center: state.Frequency, Step: swr.Steps[p.step], Count: 13 - 2*p.count}
	points := request.ValidatedPoints()
	if points == nil {
		p.status = "El rango supera los límites de sintonía"
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	reports, done := make(chan swr.Report, 16), make(chan struct{})
	p.points, p.status, p.cancel, p.reports, p.done = append([]swr.Point(nil), points...), "Preparando barrido", cancel, reports, done
	p.resultStep = request.Step
	go func() {
		defer close(done)
		defer close(reports)
		send := func(report swr.Report) { reports <- report }
		if client != nil {
			err := client.Sweep(ctx, request, send)
			switch {
			case ctx.Err() != nil:
				send(swr.Report{Status: "Barrido cancelado"})
			case err != nil:
				send(swr.Report{Status: "Error de ROE: " + err.Error()})
			}
			return
		}
		a.txSignal.swrRunning.Store(true)
		defer a.txSignal.swrRunning.Store(false)
		swr.Run(ctx, session, &a.ptt, func(duration time.Duration) {
			var until int64
			if duration > 0 {
				until = time.Now().Add(duration).UnixNano()
			}
			a.txSignal.swrToneUntil.Store(until)
		}, state, points, send)
	}()
}

func (p *swrPanel) drawButtons() {
	r := swrOverlayButton()
	if p.show {
		rl.DrawRectangleRounded(r, .2, 6, rl.NewColor(140, 24, 35, 255))
		rl.DrawRectangleRoundedLines(r, .2, 6, color.red)
		drawConsoleText(semiboldFont, "SWR", r, 13, color.text)
	} else {
		drawConsoleButtonSize(r, "SWR", false, false, 13)
	}
	drawConsoleButtonSize(swrTestButton(), "TEST", p.open, false, 12)
}

func (p *swrPanel) draw(center int64) {
	if !p.open {
		return
	}
	drawConsolePanel(rl.NewRectangle(281, 419, 1030, 331), consoleEdge)
	drawText("BARRIDO DE ROE", 309, 433, 27, color.text)
	drawConsoleButtonSize(swrCloseButton(), "X", false, false, 15)
	rl.DrawLine(304, 472, 1292, 472, consoleEdge)
	drawConsoleButton(swrStepButton(), fmt.Sprintf("STEP  %d kHz", swr.Steps[p.step]/1000), false, false)
	drawConsoleButton(swrCountButton(), fmt.Sprintf("BAR  %d", 13-2*p.count), false, false)

	play := "INICIAR"
	if p.cancel != nil {
		play = "DETENER"
	}
	drawConsoleButton(swrPlayButton(), play, p.cancel != nil, true)
	preview := swr.Frequencies(center, swr.Steps[p.step], 13-2*p.count)
	points := p.points
	if len(points) != len(preview) || p.resultStep != swr.Steps[p.step] ||
		p.cancel == nil && center > 0 && points[len(points)/2].Hz != center {
		points = preview
	}
	if points[0].Hz > 0 {
		rangeLabel := fmt.Sprintf("Rango a medir   %s–%s MHz", swrMHz(points[0].Hz), swrMHz(points[len(points)-1].Hz))
		drawConsoleText(semiboldFont, rangeLabel, rl.NewRectangle(620, 430, 580, 36), 20, color.text)
	}
	middle := len(points) / 2
	drawText("CENTRO", 306, 501, 15, color.muted)
	centerLabel := "--- MHz"
	if points[middle].Hz > 0 {
		centerLabel = fmt.Sprintf("%.3f MHz", float64(points[middle].Hz)/1e6)
	}
	drawConsoleText(displayFont, centerLabel, rl.NewRectangle(302, 521, 178, 40), 29, color.text)
	drawText("RF POWER ~30 W", 306, 567, 15, color.muted)

	graph := rl.NewRectangle(533, 496, 559, 174)
	rl.DrawRectangleRec(graph, rl.NewColor(7, 15, 24, 255))
	rl.DrawRectangleLinesEx(graph, 1, consoleEdge)
	threshold := swrY(graph, swrThreshold)
	base := swrY(graph, 1)
	for _, ratio := range []float64{1, 1.5, 2, 3} {
		y := swrY(graph, ratio)
		shade := color.muted
		if ratio == swrThreshold {
			shade = color.red
		} else {
			rl.DrawLineEx(rl.NewVector2(graph.X, y), rl.NewVector2(graph.X+graph.Width, y), 1, consoleEdge)
		}
		drawConsoleText(semiboldFont, swrScaleLabel(ratio), rl.NewRectangle(graph.X-42, y-12, 38, 24), 17, shade)
	}
	width := graph.Width / float32(len(points))
	for i, point := range points {
		x := graph.X + (float32(i)+.5)*width
		rl.DrawLineV(rl.NewVector2(x, base), rl.NewVector2(x, base+5), color.muted)
		if point.Valid {
			y := swrY(graph, swr.Ratio(point.Raw))
			bar := width * .68
			low := max(threshold, y)
			rl.DrawRectangleRec(rl.NewRectangle(x-bar/2, min(low, base-2), bar, max(2, base-low)), swrCool)
			if y < threshold {
				rl.DrawRectangleRec(rl.NewRectangle(x-bar/2, y, bar, threshold-y), swrWarm)
			}
		} else {
			drawSWRFailure(point.Failure, x, base-10)
		}
		if point.Hz > 0 && (i == 0 || i == middle || i == len(points)-1) {
			shade := color.muted
			if i == middle {
				shade = color.amber
			}
			drawConsoleText(semiboldFont, fmt.Sprintf("%.3f", float64(point.Hz)/1e6), rl.NewRectangle(x-45, base+8, 90, 23), 17, shade)
		}
	}
	rl.DrawLineEx(rl.NewVector2(graph.X, threshold), rl.NewVector2(graph.X+graph.Width, threshold), 1.6, color.red)
	rl.DrawRectangle(530, 700, 12, 12, swrCool)
	drawText("ROE medida", 550, 697, 16, color.text)
	drawSWRFailure(swr.NoTransmission, 698, 706)
	drawText("Sin transmisión", 711, 697, 16, color.text)
	drawSWRFailure(swr.NoReading, 902, 706)
	drawText("Sin lectura de ROE", 915, 697, 16, color.text)
	drawText(p.status, 306, 726, 17, color.text)
}

func swrMHz(hz int64) string {
	fraction := fmt.Sprintf("%06d", hz%1000000)
	return fmt.Sprintf("%d,%s%s", hz/1000000, fraction[:3], strings.TrimRight(fraction[3:], "0"))
}

func drawSWRFailure(failure swr.PointFailure, x, y float32) {
	switch failure {
	case swr.NoTransmission:
		top := rl.NewVector2(x, y-7)
		left, right := rl.NewVector2(x-7, y+5), rl.NewVector2(x+7, y+5)
		rl.DrawLineEx(top, left, 2, color.amber)
		rl.DrawLineEx(left, right, 2, color.amber)
		rl.DrawLineEx(right, top, 2, color.amber)
	case swr.NoReading:
		rl.DrawLineEx(rl.NewVector2(x-5, y-5), rl.NewVector2(x+5, y+5), 2, color.muted)
		rl.DrawLineEx(rl.NewVector2(x+5, y-5), rl.NewVector2(x-5, y+5), 2, color.muted)
	}
}

func swrScaleLabel(ratio float64) string {
	if ratio == 3 {
		return "3.0+"
	}
	return fmt.Sprintf("%.1f", ratio)
}

// Like the hardware display, spread 1.0, 1.5, 2.0 and 3.0 evenly.
func swrY(graph rl.Rectangle, ratio float64) float32 {
	height := ratio / 3
	if ratio <= 2 {
		height = (ratio - 1) * 2 / 3
	}
	return graph.Y + (1-float32(height))*graph.Height
}

const swrHighThreshold = 2.0

var swrZoneColors = [...]rl.Color{
	rl.NewColor(75, 223, 130, 255),
	rl.NewColor(255, 191, 65, 255),
	rl.NewColor(255, 85, 96, 255),
}

func swrZone(ratio float64) int {
	if ratio <= swrThreshold {
		return 0
	}
	if ratio <= swrHighThreshold {
		return 1
	}
	return 2
}

func drawSWRSegment(a, b rl.Vector2, graph rl.Rectangle, right float32) {
	if b.X <= a.X {
		return
	}
	start := max(0, (graph.X-a.X)/(b.X-a.X))
	end := min(1, (right-a.X)/(b.X-a.X))
	if end <= start {
		return
	}
	dy := b.Y - a.Y
	if dy == 0 {
		zone := 2
		if a.Y >= swrY(graph, swrThreshold) {
			zone = 0
		} else if a.Y >= swrY(graph, swrHighThreshold) {
			zone = 1
		}
		drawSWRLine(a, b, start, end, swrZoneColors[zone])
		return
	}
	boundaries := [...]float32{
		swrY(graph, 1), swrY(graph, swrThreshold), swrY(graph, swrHighThreshold), swrY(graph, 3),
	}
	for i, tint := range swrZoneColors {
		from := (boundaries[i] - a.Y) / dy
		to := (boundaries[i+1] - a.Y) / dy
		lo, hi := max(start, min(from, to)), min(end, max(from, to))
		if lo < hi {
			drawSWRLine(a, b, lo, hi, tint)
		}
	}
}

func drawSWRLine(a, b rl.Vector2, from, to float32, tint rl.Color) {
	dx, dy := b.X-a.X, b.Y-a.Y
	rl.DrawLineEx(rl.NewVector2(a.X+from*dx, a.Y+from*dy), rl.NewVector2(a.X+to*dx, a.Y+to*dy), 2.5, tint)
}

func (p *swrPanel) overlay(graph rl.Rectangle, sweep civ.Sweep) {
	if !p.show || sweep.High <= sweep.Low {
		return
	}
	scale := rl.NewRectangle(graph.X+graph.Width-52, graph.Y, 62, graph.Height)
	rl.DrawRectangleRec(scale, color.panel)
	rl.DrawLineEx(rl.NewVector2(scale.X, scale.Y), rl.NewVector2(scale.X, scale.Y+scale.Height), 1, consoleEdge)
	drawConsoleText(semiboldFont, "ROE", rl.NewRectangle(scale.X, scale.Y-16, scale.Width, 14), 15, color.text)
	limits := [...]float64{1, swrThreshold, swrHighThreshold, 3}
	zones := [...]string{"BAJA", "TOL.", "ALTA"}
	for i, tint := range swrZoneColors {
		bottom, top := swrY(graph, limits[i]), swrY(graph, limits[i+1])
		rl.DrawRectangleRec(rl.NewRectangle(scale.X+1, top, 4, bottom-top), tint)
		drawConsoleText(semiboldFont, zones[i], rl.NewRectangle(scale.X+7, (top+bottom)/2-9, 52, 18), 13, tint)
	}
	for _, ratio := range limits {
		y := swrY(graph, ratio)
		rl.DrawLineEx(rl.NewVector2(scale.X+5, y), rl.NewVector2(scale.X+12, y), 1, color.muted)
		labelY := max(graph.Y, min(graph.Y+graph.Height-18, y-9))
		drawConsoleText(semiboldFont, swrScaleLabel(ratio), rl.NewRectangle(scale.X+12, labelY, 46, 18), 14, color.text)
	}
	position := func(point swr.Point) rl.Vector2 {
		x := graph.X + graph.Width*float32(float64(point.Hz-sweep.Low)/float64(sweep.High-sweep.Low))
		return rl.NewVector2(x, swrY(graph, swr.Ratio(point.Raw)))
	}
	for _, saved := range p.history {
		if completeSWRSweep(saved.Points) {
			drawSWRPoints(saved.Points, graph, scale.X-2, position)
		}
	}
}

func drawSWRPoints(points []swr.Point, graph rl.Rectangle, right float32, position func(swr.Point) rl.Vector2) {
	for i, point := range points {
		if !point.Valid {
			continue
		}
		pos := position(point)
		if i > 0 && points[i-1].Valid {
			drawSWRSegment(position(points[i-1]), pos, graph, right)
		}
		if pos.X >= graph.X && pos.X < right {
			rl.DrawCircleV(pos, 3, swrZoneColors[swrZone(swr.Ratio(point.Raw))])
		}
	}
}
