package main

import (
	"context"
	"fmt"
	"math"
	"math/bits"
	"net/url"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/LuislopezMartinez/IC-RADIO/internal/civ"
	"github.com/LuislopezMartinez/IC-RADIO/internal/dxcluster"
	rl "github.com/gen2brain/raylib-go/raylib"
)

const (
	spotLifetime          = 10 * time.Minute
	clusterLabelHeight    = 20
	clusterWaterfallInset = 5
)

type clusterSpotKey struct {
	call string
	hz   int64
}

type clusterPanel struct {
	stop                                         context.CancelFunc
	events                                       <-chan dxcluster.Event
	webCancel                                    context.CancelFunc
	webResult                                    <-chan clusterWebResult
	webBand                                      string
	webNext                                      time.Time
	webStatus                                    string
	spots                                        map[clusterSpotKey]dxcluster.Spot
	status                                       string
	call, password                               string
	login, editingPassword, zoneOpen, configured bool
	zones                                        clusterZones
	group                                        []dxcluster.Spot
	groupX                                       float32
	menuSpot                                     dxcluster.Spot
	menuPosition                                 rl.Vector2
	labels                                       []clusterLabel
	view                                         clusterView
	flags                                        map[string][]byte
	textures                                     map[string]rl.Texture2D
}

type clusterWebResult struct {
	spots []dxcluster.Spot
	err   error
}

var clusterBands = map[byte]string{
	1: "160", 2: "80", 3: "40", 4: "30", 5: "20", 6: "17", 7: "15", 8: "12", 9: "10", 10: "6",
}

type clusterView struct {
	low, high, sweepLow, sweepHigh int64
	zones                          clusterZones
}

type clusterLabel struct {
	rect  rl.Rectangle
	spots []dxcluster.Spot
}

func clusterButton() rl.Rectangle       { return rl.NewRectangle(344, 240, 94, 36) }
func clusterZoneButton() rl.Rectangle   { return rl.NewRectangle(442, 240, 94, 36) }
func clusterZoneRow(i int) rl.Rectangle { return rl.NewRectangle(442, 278+float32(i)*29, 235, 28) }

var clusterZoneShort = [...]string{"ALL", "NA", "CA", "SA", "EU", "AF", "AS", "OC", "AN"}

type clusterZones uint16

func (zones clusterZones) checked(zone dxcluster.Zone) bool {
	if zone == dxcluster.All {
		return zones == 0
	}
	return zones&(1<<zone) != 0
}

func (zones clusterZones) includes(zone dxcluster.Zone) bool {
	return zones == 0 || zones.checked(zone)
}

func (zones clusterZones) toggle(zone dxcluster.Zone) clusterZones {
	if zone == dxcluster.All {
		return 0
	}
	return zones ^ (1 << zone)
}

func (zones clusterZones) summary() string {
	switch n := bits.OnesCount16(uint16(zones)); n {
	case 0:
		return "ALL"
	case 1:
		return clusterZoneShort[bits.TrailingZeros16(uint16(zones))]
	default:
		return fmt.Sprint(n)
	}
}

func (c *clusterPanel) start() {
	ctx, cancel := context.WithCancel(context.Background())
	c.stop = cancel
	c.spots = make(map[clusterSpotKey]dxcluster.Spot)
	c.webStatus = ""
	c.status = ""
	if c.configured {
		events := make(chan dxcluster.Event, 128)
		c.events = events
		go dxcluster.Run(ctx, c.call, c.password, events)
	}
}

func (c *clusterPanel) stopWeb() {
	if c.webCancel != nil {
		c.webCancel()
	}
	c.webCancel, c.webResult = nil, nil
	c.webBand, c.webNext = "", time.Time{}
}

func (c *clusterPanel) updateWeb(band string, now time.Time) {
	if c.webBand != band {
		c.stopWeb()
		c.webBand, c.webStatus = band, ""
	}
	if band == "" {
		return
	}
	if c.webResult != nil {
		select {
		case result := <-c.webResult:
			c.webCancel()
			c.webCancel, c.webResult = nil, nil
			if result.err != nil {
				c.webStatus = "DXFun web sin respuesta"
			} else {
				c.webStatus = "DXFun actualizado"
				for _, spot := range result.spots {
					c.addSpot(spot, now)
				}
			}
		default:
		}
		return
	}
	if now.Before(c.webNext) {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan clusterWebResult, 1)
	c.webCancel, c.webResult = cancel, result
	c.webNext = now.Add(time.Minute)
	go func() {
		spots, err := dxcluster.Recent(ctx, band)
		result <- clusterWebResult{spots, err}
	}()
}

func (c *clusterPanel) addSpot(spot dxcluster.Spot, now time.Time) {
	if now.Sub(spot.At) >= spotLifetime {
		return
	}
	key := clusterSpotKey{spot.Call, spot.Hz}
	if old, ok := c.spots[key]; !ok || spot.At.After(old.At) {
		c.spots[key] = spot
	}
}

func (c *clusterPanel) disconnect() {
	if c.stop != nil {
		c.stop()
	}
	c.stopWeb()
	c.stop, c.events, c.spots = nil, nil, nil
	c.group = nil
	c.menuSpot = dxcluster.Spot{}
	c.labels = nil
}

func (c *clusterPanel) closeMenus() {
	c.login, c.zoneOpen = false, false
	c.group = nil
	c.menuSpot = dxcluster.Spot{}
}

func (c *clusterPanel) updateSpots(now time.Time) {
	for i := 0; i < 128 && c.events != nil; i++ {
		select {
		case event, ok := <-c.events:
			if !ok {
				c.events = nil
				break
			}
			if event.Status != "" {
				c.status = event.Status
			} else {
				c.addSpot(event.Spot, now)
			}
		default:
			i = 128
		}
	}
	for key, spot := range c.spots {
		if now.Sub(spot.At) >= spotLifetime {
			delete(c.spots, key)
		}
	}
}

func (r *radioPanel) clusterLimits(state civ.State) (int64, int64, bool) {
	if state.Sweep.High <= state.Sweep.Low || time.Since(state.Sweep.At) >= 2*time.Second {
		return 0, 0, false
	}
	hz, exact := displayedFrequency(state, true, time.Now())
	if !exact {
		return 0, 0, false
	}
	band := bandAtFrequency(hz)
	if band < 0 {
		return 0, 0, false
	}
	limits, ok := coverageLimits[radioBands[band].code]
	lo, hi := max(limits[0], state.Sweep.Low), min(limits[1], state.Sweep.High)
	return lo, hi, ok && lo <= hi
}

func clusterHasFlag(spot dxcluster.Spot) bool {
	return spot.Entity.Flag != ""
}

func (r *radioPanel) currentClusterView(state civ.State) (clusterView, bool) {
	lo, hi, ok := r.clusterLimits(state)
	return clusterView{lo, hi, state.Sweep.Low, state.Sweep.High, r.cluster.zones}, ok
}

func clusterAgeY(wf rl.Rectangle, age time.Duration) float32 {
	fraction := float32(clampClusterAge(age)) / float32(spotLifetime)
	travel := wf.Height - clusterLabelHeight - 2*clusterWaterfallInset
	return wf.Y + clusterWaterfallInset + fraction*travel
}

func clampClusterAge(age time.Duration) time.Duration {
	return min(spotLifetime, max(0, age))
}

func (r *radioPanel) layoutClusterLabels(state civ.State, now time.Time) []clusterLabel {
	c := &r.cluster
	view, ok := r.currentClusterView(state)
	if !ok || c.stop == nil || len(c.spots) == 0 {
		c.labels = nil
		return nil
	}
	spots := make([]dxcluster.Spot, 0, len(c.spots))
	for _, spot := range c.spots {
		if now.Sub(spot.At) < spotLifetime && spot.Hz >= view.low && spot.Hz <= view.high &&
			view.zones.includes(spot.Entity.Zone) {
			spots = append(spots, spot)
		}
	}
	sort.Slice(spots, func(i, j int) bool {
		left, right := spots[i], spots[j]
		if !left.At.Equal(right.At) {
			return left.At.After(right.At)
		}
		if left.Hz != right.Hz {
			return left.Hz < right.Hz
		}
		return left.Call < right.Call
	})
	wf := rfWaterfall()
	labels := make([]clusterLabel, 0, len(spots))
	for _, spot := range spots {
		x := rfFrequencyX(wf, state.Sweep, spot.Hz)
		width := rl.MeasureTextEx(semiboldFont, spot.Call, 15, 1).X + 14
		if clusterHasFlag(spot) {
			width += 25
		}
		width = max(width, 40)
		left := max(wf.X, min(x-width/2, wf.X+wf.Width-width))
		rect := rl.NewRectangle(left, clusterAgeY(wf, now.Sub(spot.At)), width, clusterLabelHeight)
		labels = addClusterLabel(labels, rect, spot)
	}
	c.labels, c.view = labels, view
	return labels
}

func addClusterLabel(labels []clusterLabel, rect rl.Rectangle, spot dxcluster.Spot) []clusterLabel {
	padded := rl.NewRectangle(rect.X-3, rect.Y-2, rect.Width+6, rect.Height+4)
	for i := range labels {
		if rl.CheckCollisionRecs(labels[i].rect, padded) {
			labels[i].spots = append(labels[i].spots, spot)
			return labels
		}
	}
	return append(labels, clusterLabel{rect: rect, spots: []dxcluster.Spot{spot}})
}

func drawClusterTimeScale(wf rl.Rectangle) {
	for _, minute := range [...]int{0, 5, 10} {
		y := clusterAgeY(wf, time.Duration(minute)*time.Minute) + clusterLabelHeight/2
		rl.DrawLineEx(rl.NewVector2(wf.X-7, y), rl.NewVector2(wf.X, y), 1, color.muted)
		drawConsoleText(semiboldFont, fmt.Sprintf("%d min", minute), rl.NewRectangle(wf.X-64, y-9, 54, 18), 14, color.muted)
	}
}

func clusterLoginBox() rl.Rectangle        { return rl.NewRectangle(344, 280, 380, 136) }
func clusterLoginField(i int) rl.Rectangle { return rl.NewRectangle(470, 310+float32(i)*33, 240, 29) }

func clusterConnectButton() rl.Rectangle { return rl.NewRectangle(601, 380, 109, 27) }

func (c *clusterPanel) editLogin(mouse rl.Vector2) {
	if rl.IsKeyPressed(rl.KeyEscape) {
		c.login = false
		return
	}
	for i := 0; i < 2; i++ {
		if clicked(mouse, clusterLoginField(i)) {
			c.editingPassword = i == 1
		}
	}
	if rl.IsKeyPressed(rl.KeyTab) {
		c.editingPassword = !c.editingPassword
	}
	field := &c.call
	if c.editingPassword {
		field = &c.password
	}
	for key := rl.GetCharPressed(); key != 0; key = rl.GetCharPressed() {
		ch := rune(key)
		switch {
		case c.editingPassword && len(*field) < 64 && ch >= 32 && ch <= 126:
			*field += string(ch)
		case !c.editingPassword && len(*field) < 15:
			ch = unicode.ToUpper(ch)
			if strings.ContainsRune("ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-", ch) {
				*field += string(ch)
			}
		}
	}
	if rl.IsKeyPressed(rl.KeyBackspace) && len(*field) > 0 {
		*field = (*field)[:len(*field)-1]
	}
	if (rl.IsKeyPressed(rl.KeyEnter) || clicked(mouse, clusterConnectButton())) && dxcluster.ValidLogin(c.call) {
		c.login, c.configured = false, true
		c.start()
		return
	}
	if rl.IsMouseButtonPressed(rl.MouseLeftButton) && !rl.CheckCollisionPointRec(mouse, clusterLoginBox()) {
		c.login = false
	}
}

func (c *clusterPanel) groupRow(i int) rl.Rectangle {
	x := min(c.groupX, rfWaterfall().X+rfWaterfall().Width-250)
	return rl.NewRectangle(x, rfWaterfall().Y-float32(min(len(c.group), 10)-i)*26, 250, 25)
}

func (c *clusterPanel) openSpotMenu(spot dxcluster.Spot, mouse rl.Vector2) {
	waterfall := rfWaterfall()
	c.menuSpot = spot
	c.menuPosition = rl.NewVector2(
		min(mouse.X, waterfall.X+waterfall.Width-132),
		min(mouse.Y, waterfall.Y+waterfall.Height-54),
	)
}

func (c *clusterPanel) spotMenuRow(i int) rl.Rectangle {
	return rl.NewRectangle(c.menuPosition.X, c.menuPosition.Y+float32(i)*27, 132, 26)
}

func (a *app) updateCluster(mouse rl.Vector2, state civ.State, online, canTune bool) bool {
	r := &a.radio
	c := &r.cluster
	now := time.Now()
	c.updateSpots(now)
	if c.stop != nil {
		band := ""
		if online {
			if i := bandAtFrequency(state.Frequency); i >= 0 {
				band = clusterBands[radioBands[i].code]
			}
		}
		c.updateWeb(band, now)
	}
	if c.login {
		c.editLogin(mouse)
		return true
	}
	if c.zoneOpen {
		if rl.IsKeyPressed(rl.KeyEscape) {
			c.zoneOpen = false
		} else if rl.IsMouseButtonPressed(rl.MouseLeftButton) {
			for i := range dxcluster.ZoneNames {
				if clicked(mouse, clusterZoneRow(i)) {
					c.zones = c.zones.toggle(dxcluster.Zone(i))
					return true
				}
			}
			c.zoneOpen = false
		}
		return true
	}
	if c.menuSpot.Call != "" {
		if rl.IsKeyPressed(rl.KeyEscape) {
			c.menuSpot = dxcluster.Spot{}
		} else if rl.IsMouseButtonPressed(rl.MouseLeftButton) {
			spot := c.menuSpot
			c.menuSpot = dxcluster.Spot{}
			switch {
			case canTune && clicked(mouse, c.spotMenuRow(0)):
				a.tuneClusterSpot(state, spot)
			case clicked(mouse, c.spotMenuRow(1)):
				rl.OpenURL("https://www.qrz.com/db/" + url.PathEscape(spot.Call))
			}
		}
		return true
	}
	if len(c.group) > 0 {
		if rl.IsKeyPressed(rl.KeyEscape) {
			c.group = nil
		} else if rl.IsMouseButtonPressed(rl.MouseLeftButton) {
			for i, spot := range c.group[:min(len(c.group), 10)] {
				if clicked(mouse, c.groupRow(i)) {
					c.openSpotMenu(spot, mouse)
					break
				}
			}
			c.group = nil
		} else if wheel := rl.GetMouseWheelMove(); wheel != 0 && len(c.group) > 10 {
			if wheel < 0 {
				c.group = append(c.group[1:], c.group[0])
			} else {
				last := c.group[len(c.group)-1]
				copy(c.group[1:], c.group)
				c.group[0] = last
			}
		}
		return true
	}
	if rl.IsMouseButtonPressed(rl.MouseRightButton) && rl.CheckCollisionPointRec(mouse, clusterButton()) {
		c.disconnect()
		c.login, c.editingPassword = true, false
		return true
	}
	if clicked(mouse, clusterButton()) {
		if c.stop != nil {
			c.disconnect()
		} else {
			c.start()
		}
		return true
	}
	if clicked(mouse, clusterZoneButton()) {
		c.zoneOpen = true
		return true
	}
	view, valid := r.currentClusterView(state)
	if online && valid && view == c.view && rl.IsMouseButtonPressed(rl.MouseLeftButton) {
		for _, label := range c.labels {
			if !rl.CheckCollisionPointRec(mouse, label.rect) {
				continue
			}
			if len(label.spots) == 1 {
				c.openSpotMenu(label.spots[0], mouse)
			} else {
				c.group = append([]dxcluster.Spot(nil), label.spots...)
				c.groupX = label.rect.X
			}
			return true
		}
	}
	return false
}

func (a *app) tuneClusterSpot(state civ.State, spot dxcluster.Spot) {
	r := &a.radio
	lo, hi, ok := r.clusterLimits(state)
	if !ok || spot.Hz < lo || spot.Hz > hi || time.Since(spot.At) >= spotLifetime {
		return
	}
	r.wheel = wheelTuning{}
	if !r.commitTuning(state, spot.Hz) {
		a.message = "Could not tune to " + spot.Call
	}
}

func (c *clusterPanel) drawFlag(code string, x, y float32) bool {
	if code == "ic" {
		for i, shade := range [...]rl.Color{rl.White, rl.NewColor(0, 85, 170, 255), rl.NewColor(255, 215, 0, 255)} {
			rl.DrawRectangleRec(rl.NewRectangle(x+float32(i)*7, y, 7, 13), shade)
		}
		return true
	}
	if c.textures == nil {
		c.flags = dxcluster.Flags()
		c.textures = make(map[string]rl.Texture2D)
	}
	texture, cached := c.textures[code]
	if !cached {
		if png := c.flags[code]; len(png) > 0 {
			image := rl.LoadImageFromMemory(".png", png, int32(len(png)))
			texture = rl.LoadTextureFromImage(image)
			rl.UnloadImage(image)
		}
		c.textures[code] = texture
	}
	if texture.ID == 0 {
		return false
	}
	height := float32(13)
	width := min(21, height*float32(texture.Width)/float32(texture.Height))
	rl.DrawTexturePro(texture, rl.NewRectangle(0, 0, float32(texture.Width), float32(texture.Height)), rl.NewRectangle(x, y, width, height), rl.Vector2{}, 0, rl.White)
	return true
}

func (r *radioPanel) drawClusterLabels(state civ.State, mouse rl.Vector2) {
	c := &r.cluster
	var hover *dxcluster.Spot
	labels := r.layoutClusterLabels(state, time.Now())
	if c.stop != nil {
		drawClusterTimeScale(rfWaterfall())
	}
	if c.stop != nil && len(labels) == 0 {
		status := "Sin spots recientes en el tramo visible"
		switch {
		case c.webResult != nil && c.webStatus == "":
			status = "Cargando spots de DXFun…"
		case c.webStatus == "DXFun web sin respuesta" && len(c.spots) == 0:
			status = c.webStatus
		case c.zones != 0:
			status += " · filtro de zonas activo"
		}
		wf := rfWaterfall()
		drawText(status, int32(wf.X+8), int32(wf.Y+4), 15, color.muted)
	}
	for _, label := range labels {
		rec := label.rect
		active := rl.CheckCollisionPointRec(mouse, rec) && !c.zoneOpen && !c.login && len(c.group) == 0 && c.menuSpot.Call == ""
		drawCutPanel(rec, 3, color.panel2, consoleEdge, 1)
		spot := label.spots[0]
		x := rfFrequencyX(rfWaterfall(), state.Sweep, spot.Hz)
		rl.DrawLineEx(rl.NewVector2(x, rec.Y+rec.Height), rl.NewVector2(x, rec.Y+rec.Height+3), 1, color.muted)
		if len(label.spots) > 1 {
			drawConsoleText(semiboldFont, fmt.Sprintf("+%d", len(label.spots)), rec, 15, color.text)
			continue
		}
		if active {
			hover = &spot
		}
		if clusterHasFlag(spot) && c.drawFlag(spot.Entity.Flag, rec.X+5, rec.Y+3) {
			rec.X += 23
			rec.Width -= 23
		}
		shade := color.text
		if active {
			shade = color.green
		}
		drawConsoleText(semiboldFont, spot.Call, rec, 15, shade)
	}
	if hover != nil {
		age := max(0, int(math.Floor(time.Since(hover.At).Minutes())))
		entity := hover.Entity.Name
		if entity == "" {
			entity = "DXCC desconocida"
		}
		line := fmt.Sprintf("%s · %s · %d min · de %s", clusterFrequency(hover.Hz), entity, age, hover.Reporter)
		drawClusterInfo(line, hover.Comment, mouse.X, rfWaterfall().Y-53)
	}
}

func clusterFrequency(hz int64) string {
	return fmt.Sprintf("%d,%03d kHz", hz/1000, hz%1000)
}

func drawClusterInfo(line, detail string, x, y float32) {
	width := float32(420)
	x = min(x, rfWaterfall().X+rfWaterfall().Width-width)
	drawCutPanel(rl.NewRectangle(x, y, width, 48), 4, color.panel, consoleEdge, 1)
	drawText(ellipsis(line, 51), int32(x+8), int32(y+5), 15, color.text)
	drawText(ellipsis(detail, 51), int32(x+8), int32(y+24), 15, color.muted)
}

func (c *clusterPanel) drawMenus(mouse rl.Vector2) {
	switch {
	case c.login:
		box := clusterLoginBox()
		drawCutPanel(box, 4, color.panel, consoleEdge, 1)
		drawText("DXFun · acceso de lectura", 354, 288, 16, color.green)
		drawText("Indicativo", 354, 317, 15, color.text)
		drawText("Clave opcional", 354, 350, 15, color.text)
		drawConsoleButtonSize(clusterLoginField(0), c.call, !c.editingPassword, false, 15)
		drawConsoleButtonSize(clusterLoginField(1), strings.Repeat("*", len(c.password)), c.editingPassword, false, 15)
		drawText("Esc para cerrar", 354, 388, 15, color.muted)
		drawConsoleButtonSize(clusterConnectButton(), "CONECTAR", false, dxcluster.ValidLogin(c.call), 15)
	case c.zoneOpen:
		for i, label := range dxcluster.ZoneNames {
			row := clusterZoneRow(i)
			drawConsoleButtonSize(row, "", false, false, 15)
			box := rl.NewRectangle(row.X+9, row.Y+7, 14, 14)
			rl.DrawRectangleLinesEx(box, 1, color.muted)
			if c.zones.checked(dxcluster.Zone(i)) {
				rl.DrawRectangleRec(rl.NewRectangle(box.X+3, box.Y+3, 8, 8), color.green)
			}
			drawText(label, int32(row.X+32), int32(row.Y+5), 15, color.text)
		}
	case c.menuSpot.Call != "":
		for i, label := range [...]string{"Tune", "Open QRZ"} {
			rec := c.spotMenuRow(i)
			drawRFOptionSize(rec, label, rl.CheckCollisionPointRec(mouse, rec), 16)
		}
	case len(c.group) > 0:
		for i, spot := range c.group[:min(len(c.group), 10)] {
			rec := c.groupRow(i)
			drawRFOptionSize(rec, spot.Call+"   "+clusterFrequency(spot.Hz), rl.CheckCollisionPointRec(mouse, rec), 15)
			if clusterHasFlag(spot) {
				c.drawFlag(spot.Entity.Flag, rec.X+8, rec.Y+(rec.Height-13)/2)
			}
		}
	default:
		if c.stop != nil && rl.CheckCollisionPointRec(mouse, clusterButton()) {
			status := c.webStatus
			if status == "" {
				status = "DXFun · esperando datos de la banda"
			}
			drawClusterInfo(status, c.status, clusterButton().X, clusterButton().Y+38)
		}
	}
}

func (c *clusterPanel) drawButtons() {
	button := clusterButton()
	if c.stop == nil {
		drawConsoleButtonSize(button, "CLUSTER", false, false, 16)
	} else {
		rl.DrawRectangleRounded(button, .22, 8, color.green)
		drawConsoleText(semiboldFont, "CLUSTER", button, 16, color.bg)
	}
	drawConsoleButtonSize(clusterZoneButton(), "ZONA "+c.zones.summary()+" ▼", false, false, 15)
}

func (c *clusterPanel) unload() {
	c.disconnect()
	for _, texture := range c.textures {
		if texture.ID != 0 {
			rl.UnloadTexture(texture)
		}
	}
}
