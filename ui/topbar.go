package ui

import (
	"fmt"

	rl "github.com/gen2brain/raylib-go/raylib"
)

// TimeDisplay: Scale = 0 means paused; Elapsed is scaled game-time seconds.
type TimeDisplay struct {
	Scale   float32
	Elapsed float32
}

var (
	topBarBG      = rl.Color{R: 18, G: 22, B: 28, A: 255}
	topBarPauseBG = rl.Color{R: 70, G: 28, B: 28, A: 255}
	topBarBorder  = rl.Color{R: 30, G: 30, B: 36, A: 255}
	topBarText    = rl.Color{R: 230, G: 235, B: 240, A: 255}
	topBarTextDim = rl.Color{R: 140, G: 150, B: 160, A: 255}
	topBarAccent  = rl.Color{R: 80, G: 180, B: 240, A: 255}
	topBarBtnIdle = rl.Color{R: 30, G: 36, B: 44, A: 255}
	topBarBtnHot  = rl.Color{R: 60, G: 80, B: 110, A: 255}
)

type TopBarHitKind uint8

const (
	TopBarHitNone TopBarHitKind = iota
	TopBarHitPlayPause
	TopBarHitSpeedDown
	TopBarHitSpeedUp
	TopBarHitToolSE       // Symbol Editor
	TopBarHitToolFE       // Formation Editor
	TopBarHitToolSettings // Settings panel (placeholder for Phase 18.5.E follow-up)
)

// TopBarHits bundles the click-targetable regions DrawTopBar painted on the
// last frame. main.go feeds these back into TopBarHitTest to route LMB.
type TopBarHits struct {
	PlayPause    rl.Rectangle
	SpeedDown    rl.Rectangle
	SpeedUp      rl.Rectangle
	ToolSE       rl.Rectangle
	ToolFE       rl.Rectangle
	ToolSettings rl.Rectangle
}

// TopBarToolCtx flags the disabled / active state of each toolbar button.
// Disabled buttons render dimmed and ignore clicks. Active = the widget is
// already on-screen somewhere (workspace or floating); button gets a subtle
// underline.
type TopBarToolCtx struct {
	SEActive    bool
	SEEnabled   bool // Enabled when a single Unit / Contact is selected.
	FEActive    bool
	FEEnabled   bool // Enabled when a squad is selected.
	SettingsOn  bool
	SettingsCan bool
}

// DrawTopBar returns the hit-rects so main.go can route LMB clicks back to
// TimeScale mutations + toolbar actions without re-deriving geometry.
func DrawTopBar(panel Panel, font rl.Font, d TimeDisplay, tools TopBarToolCtx,
	cursor rl.Vector2) TopBarHits {
	r := panel.Bounds
	bg := topBarBG
	if d.Scale <= 0 {
		bg = topBarPauseBG
	}
	rl.DrawRectangleRec(r, bg)
	rl.DrawLine(int32(r.X), int32(r.Y+r.Height-1),
		int32(r.X+r.Width), int32(r.Y+r.Height-1), topBarBorder)

	cy := r.Y + r.Height*0.5

	// Action toolbar — left.
	const toolW float32 = 28
	const toolGap float32 = 4
	toolY := r.Y + (r.Height-toolW)*0.5
	tx := r.X + 8
	hits := TopBarHits{}
	hits.ToolSE = rl.Rectangle{X: tx, Y: toolY, Width: toolW, Height: toolW}
	drawTopBarToolButton(hits.ToolSE, "SE", font, tools.SEEnabled, tools.SEActive,
		"Symbol Editor (select unit/contact)", cursor)
	tx += toolW + toolGap

	hits.ToolFE = rl.Rectangle{X: tx, Y: toolY, Width: toolW, Height: toolW}
	drawTopBarToolButton(hits.ToolFE, "FE", font, tools.FEEnabled, tools.FEActive,
		"Formation Editor (select squad)", cursor)
	tx += toolW + toolGap

	hits.ToolSettings = rl.Rectangle{X: tx, Y: toolY, Width: toolW, Height: toolW}
	drawTopBarToolButton(hits.ToolSettings, "⚙", font, tools.SettingsCan, tools.SettingsOn,
		"Settings (placeholder)", cursor)

	// Right-aligned cluster: clock + speed controls.
	const clockSize int32 = 16
	mins := int(d.Elapsed) / 60
	secs := int(d.Elapsed) % 60
	clock := fmt.Sprintf("T+%02d:%02d", mins, secs)
	clockW := rl.MeasureTextEx(font, clock, float32(clockSize), 1.0).X

	const btnW float32 = 26
	const gap float32 = 7
	label := fmt.Sprintf("%.0fx", d.Scale)
	if d.Scale <= 0 {
		label = "PAUSED"
	}
	const labelSize int32 = 16
	labelW := rl.MeasureTextEx(font, label, float32(labelSize), 1.0).X
	rightW := clockW + gap*3 + btnW*3 + labelW
	rightX := r.X + r.Width - rightW - 10

	rl.DrawTextEx(font, clock,
		rl.Vector2{X: rightX, Y: cy - float32(clockSize)*0.5},
		float32(clockSize), 1.0, topBarText)
	startX := rightX + clockW + gap
	btnY := r.Y + (r.Height-btnW)*0.5

	hits.SpeedDown = rl.Rectangle{X: startX, Y: btnY, Width: btnW, Height: btnW}
	drawTopBarButton(hits.SpeedDown, "<<", font)

	hits.PlayPause = rl.Rectangle{X: startX + btnW + gap, Y: btnY, Width: btnW, Height: btnW}
	drawTopBarButton(hits.PlayPause, "", font)
	if d.Scale <= 0 {
		drawPlayGlyph(hits.PlayPause, topBarAccent)
	} else {
		drawPauseGlyph(hits.PlayPause, topBarAccent)
	}

	labelX := hits.PlayPause.X + btnW + gap
	rl.DrawTextEx(font, label,
		rl.Vector2{X: labelX, Y: cy - float32(labelSize)*0.5},
		float32(labelSize), 1.0, topBarText)

	hits.SpeedUp = rl.Rectangle{X: labelX + labelW + gap, Y: btnY, Width: btnW, Height: btnW}
	drawTopBarButton(hits.SpeedUp, ">>", font)
	return hits
}

// TopBarHitTest uses the rects returned from DrawTopBar last frame.
func TopBarHitTest(cursor rl.Vector2, hits TopBarHits) TopBarHitKind {
	if pointInRect(cursor, hits.ToolSE) {
		return TopBarHitToolSE
	}
	if pointInRect(cursor, hits.ToolFE) {
		return TopBarHitToolFE
	}
	if pointInRect(cursor, hits.ToolSettings) {
		return TopBarHitToolSettings
	}
	if pointInRect(cursor, hits.PlayPause) {
		return TopBarHitPlayPause
	}
	if pointInRect(cursor, hits.SpeedDown) {
		return TopBarHitSpeedDown
	}
	if pointInRect(cursor, hits.SpeedUp) {
		return TopBarHitSpeedUp
	}
	return TopBarHitNone
}

func drawTopBarButton(r rl.Rectangle, glyph string, font rl.Font) {
	rl.DrawRectangleRec(r, topBarBtnIdle)
	rl.DrawRectangleLinesEx(r, 1, topBarBorder)
	if glyph != "" {
		const sz int32 = 14
		m := rl.MeasureTextEx(font, glyph, float32(sz), 1.0)
		rl.DrawTextEx(font, glyph,
			rl.Vector2{X: r.X + (r.Width-m.X)*0.5, Y: r.Y + (r.Height-m.Y)*0.5},
			float32(sz), 1.0, topBarText)
	}
}

// drawTopBarToolButton paints an action button. Disabled = 40% alpha and
// no hover highlight. Active = small underline strip at bottom.
func drawTopBarToolButton(r rl.Rectangle, glyph string, font rl.Font,
	enabled, active bool, _ string, cursor rl.Vector2) {
	hover := enabled && rl.CheckCollisionPointRec(cursor, r)
	bg := topBarBtnIdle
	if hover {
		bg = topBarBtnHot
	}
	if !enabled {
		bg = rl.Color{R: 24, G: 28, B: 34, A: 255}
	}
	rl.DrawRectangleRec(r, bg)
	rl.DrawRectangleLinesEx(r, 1, topBarBorder)
	col := topBarText
	if !enabled {
		col = topBarTextDim
	}
	const sz int32 = 13
	m := rl.MeasureTextEx(font, glyph, float32(sz), 1.0)
	rl.DrawTextEx(font, glyph,
		rl.Vector2{X: r.X + (r.Width-m.X)*0.5, Y: r.Y + (r.Height-m.Y)*0.5},
		float32(sz), 1.0, col)
	if active {
		rl.DrawRectangle(int32(r.X+3), int32(r.Y+r.Height-3),
			int32(r.Width-6), 2, topBarAccent)
	}
}

func drawPlayGlyph(r rl.Rectangle, c rl.Color) {
	pad := r.Width * 0.28
	v1 := rl.Vector2{X: r.X + pad, Y: r.Y + pad}
	v2 := rl.Vector2{X: r.X + pad, Y: r.Y + r.Height - pad}
	v3 := rl.Vector2{X: r.X + r.Width - pad, Y: r.Y + r.Height*0.5}
	rl.DrawTriangle(v1, v2, v3, c)
}

func drawPauseGlyph(r rl.Rectangle, c rl.Color) {
	pad := r.Width * 0.28
	barW := (r.Width - 2*pad) / 3
	rl.DrawRectangle(int32(r.X+pad), int32(r.Y+pad),
		int32(barW), int32(r.Height-2*pad), c)
	rl.DrawRectangle(int32(r.X+r.Width-pad-barW), int32(r.Y+pad),
		int32(barW), int32(r.Height-2*pad), c)
}
