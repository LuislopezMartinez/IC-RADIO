package main

import (
	"fmt"
	"strconv"
	"time"

	"github.com/LuislopezMartinez/IC-RADIO/internal/civ"
	rl "github.com/gen2brain/raylib-go/raylib"
)

var (
	filterButton = rl.NewRectangle(139, 176, 144, 29)
	filterMenu   = rl.NewRectangle(139, 211, 246, 117)
	filterWidth  = rl.NewRectangle(194, 266, 136, 38)
	filterMinus  = rl.NewRectangle(151, 266, 37, 38)
	filterPlus   = rl.NewRectangle(336, 266, 37, 38)
)

type radioFilterPanel struct {
	open   bool
	base   civ.FilterState
	target int
	sentAt time.Time
	drag   filterEdgeDrag
}

type filterEdgeDrag struct {
	active  bool
	base    civ.FilterState
	width   int
	originX float32
	span    int64
	rx      int64
	until   time.Time
}

func filterSlotRect(i int) rl.Rectangle {
	return rl.NewRectangle(151+float32(i)*75, 222, 69, 36)
}

func widthAvailable(f civ.FilterState, now time.Time) bool {
	return f.SSB() && recentTuning(now, f.ModeAt) && recentTuning(now, f.WidthAt)
}

func (a *app) updateRadioFilter(mouse rl.Vector2, state civ.State, available bool) bool {
	r := &a.radio
	panel, f, now := &r.ifFilter, state.IF, time.Now()
	if !available {
		*panel = radioFilterPanel{}
		return false
	}
	if clicked(mouse, filterButton) {
		panel.open = !panel.open
		return true
	}
	if panel.open {
		if clicked(mouse, filterWidth) && widthAvailable(f, now) {
			panel.base = f
			r.editing, r.editFilterWidth, r.input = true, true, ""
			return true
		}
		if widthAvailable(f, now) {
			switch {
			case clicked(mouse, filterMinus):
				a.stepFilterWidth(f, -1, now)
			case clicked(mouse, filterPlus):
				a.stepFilterWidth(f, 1, now)
			case rl.CheckCollisionPointRec(mouse, filterWidth):
				if ticks := int(rl.GetMouseWheelMove()); ticks != 0 {
					a.stepFilterWidth(f, ticks, now)
				}
			}
		}
		for i := 0; i < 3; i++ {
			if clicked(mouse, filterSlotRect(i)) && recentTuning(now, f.ModeAt) {
				if !r.session.SetFilterSlot(byte(i + 1)) {
					a.message = "No se pudo seleccionar el filtro"
				}
				panel.sentAt = time.Time{}
				return true
			}
		}
		if rl.CheckCollisionPointRec(mouse, filterMenu) {
			return true
		}
		if rl.IsMouseButtonPressed(rl.MouseLeftButton) {
			panel.open = false
		}
	}
	return false
}

func sameFilterSlot(a, b civ.FilterState) bool {
	return a.Mode == b.Mode && a.Data == b.Data && a.Slot == b.Slot
}

// CI-V reports the bandwidth rather than the RF endpoints of the passband.
func nominalSSBEdges(rx int64, mode byte, width int) (int64, int64) {
	half := int64(width / 2)
	center := max(1500, half)
	if mode == 0 {
		return rx - center - half, rx - center + half
	}
	return rx + center - half, rx + center + half
}

func filterOuterEdge(rx int64, f civ.FilterState, width int) int64 {
	lo, hi := nominalSSBEdges(rx, f.Mode, width)
	if f.Mode == 0 {
		return lo
	}
	return hi
}

func filterEdgeHit(mouse rl.Vector2, graph rl.Rectangle, x float32) bool {
	return rl.CheckCollisionPointRec(mouse, graph) && mouse.X >= x-5 && mouse.X <= x+5
}

func (a *app) updateFilterEdge(mouse rl.Vector2, state civ.State, available bool) bool {
	r := &a.radio
	drag := &r.ifFilter.drag
	now := time.Now()
	sweep, f, graph := state.Sweep, state.IF, rfGraph()
	rx, _ := state.EffectiveTuning(now)
	if !available || rx <= 0 || !widthAvailable(f, now) || now.Sub(sweep.At) >= 2*time.Second || sweep.High <= sweep.Low {
		drag.active = false
		return false
	}
	if drag.active {
		if drag.rx != rx || drag.span != sweep.High-sweep.Low || !sameFilterSlot(drag.base, f) || f.WidthHz != drag.base.WidthHz {
			*drag = filterEdgeDrag{}
			return true
		}
		delta := rfFrequency(graph, mouse.X, sweep) - rfFrequency(graph, drag.originX, sweep)
		if f.Mode == 0 {
			delta = -delta
		}
		// The nominal outer edge moves at half the width until the band reaches the carrier.
		outer := max(1500, int64(f.WidthHz/2)) + int64(f.WidthHz/2) + delta
		width := outer
		if outer < 3000 {
			width = 2 * (outer - 1500)
		}
		drag.width = civ.SSBFilterWidth(int(width))
		if !rl.IsMouseButtonDown(rl.MouseLeftButton) {
			drag.active = false
			drag.until = now.Add(1200 * time.Millisecond)
			if drag.width != f.WidthHz && a.sendFilterWidth(drag.width) {
				r.ifFilter.base, r.ifFilter.target, r.ifFilter.sentAt = f, drag.width, now
			}
		}
		return true
	}
	edge := filterOuterEdge(rx, f, f.WidthHz)
	if edge < sweep.Low || edge > sweep.High ||
		!filterEdgeHit(mouse, graph, rfFrequencyX(graph, sweep, edge)) || !rl.IsMouseButtonPressed(rl.MouseLeftButton) {
		return false
	}
	*drag = filterEdgeDrag{active: true, base: f, width: f.WidthHz, originX: mouse.X, span: sweep.High - sweep.Low, rx: rx}
	return true
}

func (r *radioPanel) drawFilterEdge(state civ.State, rx int64, mouse rl.Vector2) {
	now := time.Now()
	f, sweep := state.IF, state.Sweep
	if rx <= 0 || !widthAvailable(f, now) {
		return
	}
	drag := r.ifFilter.drag
	width := f.WidthHz
	dragging := drag.active && sameFilterSlot(drag.base, f) && drag.rx == rx
	if dragging {
		width = drag.width
	}
	lo, hi := nominalSSBEdges(rx, f.Mode, width)
	if hi < sweep.Low || lo > sweep.High {
		return
	}
	graph := rfGraph()
	for _, area := range [...]rl.Rectangle{graph, rfWaterfall()} {
		left := rfFrequencyX(area, sweep, max(lo, sweep.Low))
		right := rfFrequencyX(area, sweep, min(hi, sweep.High))
		rl.DrawRectangleRec(rl.NewRectangle(left, area.Y, right-left, area.Height), rl.Fade(color.green, .065))
	}
	edge := filterOuterEdge(rx, f, width)
	if edge >= sweep.Low && edge <= sweep.High {
		x := rfFrequencyX(graph, sweep, edge)
		shade := rl.Fade(color.green, .35)
		if dragging || filterEdgeHit(mouse, graph, x) {
			shade = color.green
		}
		rl.DrawLineEx(rl.NewVector2(x, graph.Y), rl.NewVector2(x, graph.Y+graph.Height), 1, shade)
	}
	if dragging || now.Before(drag.until) && sameFilterSlot(drag.base, f) && drag.rx == rx {
		x := rfFrequencyX(graph, sweep, rx)
		const boxWidth = 160
		box := rl.NewRectangle(max(graph.X, min(x+8, graph.X+graph.Width-boxWidth)), graph.Y+7, boxWidth, 27)
		drawConsolePanel(box, color.green)
		drawConsoleText(semiboldFont, fmt.Sprintf("%d Hz nominal", width), box, 16, color.green)
	}
}

func (a *app) stepFilterWidth(f civ.FilterState, ticks int, now time.Time) {
	p := &a.radio.ifFilter
	width := f.WidthHz
	if !p.sentAt.IsZero() && now.Sub(p.sentAt) < time.Second && sameFilterSlot(p.base, f) {
		width = p.target
	}
	target := width
	for ; ticks > 0 && target < 3600; ticks-- {
		step := 50
		if target >= 500 {
			step = 100
		}
		target += step
	}
	for ; ticks < 0 && target > 50; ticks++ {
		step := 50
		if target > 500 {
			step = 100
		}
		target -= step
	}
	if target != width && a.sendFilterWidth(target) {
		p.base, p.target, p.sentAt = f, target, now
	}
}

func (a *app) editRadioFilterWidth(state civ.State, available bool) {
	r := &a.radio
	r.collectInput("0123456789", 4)
	if !rl.IsKeyPressed(rl.KeyEnter) && !rl.IsKeyPressed(rl.KeyKpEnter) {
		return
	}
	hz, err := strconv.Atoi(r.input)
	if err != nil || hz != civ.SSBFilterWidth(hz) {
		a.message = "Ancho SSB: 50–500 Hz de 50 en 50 o 600–3600 Hz de 100 en 100"
		return
	}
	base := r.ifFilter.base
	if !available || !widthAvailable(state.IF, time.Now()) || !sameFilterSlot(base, state.IF) || base.WidthHz != state.IF.WidthHz {
		r.editing, r.editFilterWidth = false, false
		a.message = "El filtro cambió o la radio no está en RX"
		return
	}
	if a.sendFilterWidth(hz) {
		r.editing, r.editFilterWidth = false, false
		r.ifFilter.sentAt = time.Time{}
	}
}

func (a *app) sendFilterWidth(hz int) bool {
	if a.radio.session.SetFilterWidth(hz) {
		return true
	}
	a.message = "No se pudo ajustar el ancho del filtro"
	return false
}

func (r *radioPanel) drawFilterMenu(state civ.State) {
	if !r.ifFilter.open {
		return
	}
	f := state.IF
	drawConsolePanel(filterMenu, color.green)
	for i := 0; i < 3; i++ {
		drawConsoleButton(filterSlotRect(i), fmt.Sprintf("FIL%d", i+1), f.Slot == byte(i+1), false)
	}
	label := "ANCHO  —"
	if r.editFilterWidth {
		label = r.input + "_ Hz"
	} else if widthAvailable(f, time.Now()) {
		label = fmt.Sprintf("ANCHO  %d Hz", f.WidthHz)
	}
	drawConsoleButton(filterWidth, label, r.editFilterWidth, false)
	drawConsoleButton(filterMinus, "–", false, false)
	drawConsoleButton(filterPlus, "+", false, false)
	for i, value := range f.PBT {
		if value != 128 && recentTuning(time.Now(), f.PBTAt[i]) {
			drawText("PBT activo", 151, 306, 13, color.amber)
			break
		}
	}
}
