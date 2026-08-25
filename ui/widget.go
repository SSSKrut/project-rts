package ui

import (
	rl "github.com/gen2brain/raylib-go/raylib"
)

// Immediate-mode building blocks shared by every panel. Before these, each
// surface grew its own button, section header, bar and hover test; the copies
// drifted apart in behaviour while looking the same. Widgets take a rect and
// a Style, so a surface keeps its own palette without owning its own drawing.

// WidgetInput is the pointer state an interactive widget needs. Enabled folds
// in panel focus and input shielding: when false a widget neither highlights
// nor fires, which is what keeps a drag started elsewhere from clicking through.
type WidgetInput struct {
	Cursor  rl.Vector2
	Press   bool
	Enabled bool
}

func (in WidgetInput) Hover(r rl.Rectangle) bool {
	return in.Enabled && in.Cursor.X >= r.X && in.Cursor.X < r.X+r.Width &&
		in.Cursor.Y >= r.Y && in.Cursor.Y < r.Y+r.Height
}

func (in WidgetInput) Clicked(r rl.Rectangle) bool { return in.Press && in.Hover(r) }

// Style is a surface's palette and metrics. Widgets never hard-code a colour,
// so the inspector and the debug panel share code while staying distinct.
type Style struct {
	Font       rl.Font
	FontSize   float32
	RowH       float32
	Gap        float32
	Text       rl.Color
	TextDim    rl.Color
	Header     rl.Color
	Fill       rl.Color
	FillActive rl.Color
	FillHover  rl.Color
	Border     rl.Color
	Disabled   rl.Color
}

func InspectorStyle(font rl.Font) Style {
	return Style{
		Font:       font,
		FontSize:   float32(inspectorFontSize),
		RowH:       float32(inspectorRowH),
		Gap:        float32(srChipGap),
		Text:       inspectorText,
		TextDim:    inspectorTextDim,
		Header:     srSectionHdr,
		Fill:       srChipBG,
		FillActive: srChipActive,
		FillHover:  srChipHover,
		Border:     srChipBorder,
	}
}

func DebugStyle(font rl.Font) Style {
	return Style{
		Font:       font,
		FontSize:   float32(debugRowSize),
		RowH:       debugRowHeight,
		Gap:        6,
		Text:       debugTextOn,
		TextDim:    debugTextOff,
		Header:     debugTextOn,
		Fill:       debugBtnBG,
		FillActive: debugBtnArmed,
		FillHover:  debugRowHover,
		Border:     debugCheckEdge,
	}
}

func TopBarStyle(font rl.Font) Style {
	return Style{
		Font:       font,
		FontSize:   13,
		Text:       topBarText,
		TextDim:    topBarTextDim,
		Fill:       topBarBtnIdle,
		FillActive: topBarBtnHot,
		FillHover:  topBarBtnHot,
		Border:     topBarBorder,
		Disabled:   rl.Color{R: 24, G: 28, B: 34, A: 255},
	}
}

// Column is a top-down layout cursor over a fixed-width strip. It owns the
// `y += h + gap` arithmetic that every panel used to repeat by hand.
type Column struct {
	X, Y, W float32
	Gap     float32
}

// Row reserves a full-width band of height h and advances past it.
func (c *Column) Row(h float32) rl.Rectangle {
	r := rl.Rectangle{X: c.X, Y: c.Y, Width: c.W, Height: h}
	c.Y += h + c.Gap
	return r
}

// Band is Row without the trailing gap — for rows that stack flush.
func (c *Column) Band(h float32) rl.Rectangle {
	r := rl.Rectangle{X: c.X, Y: c.Y, Width: c.W, Height: h}
	c.Y += h
	return r
}

func (c *Column) Skip(dy float32) { c.Y += dy }

// SplitX slices a row into n equal cells separated by gap.
func SplitX(r rl.Rectangle, i, n int, gap float32) rl.Rectangle {
	if n <= 0 {
		return r
	}
	w := (r.Width - float32(n-1)*gap) / float32(n)
	return rl.Rectangle{
		X: r.X + float32(i)*(w+gap), Y: r.Y, Width: w, Height: r.Height,
	}
}

// Flow lays widgets left to right inside a Column, wrapping when the next one
// would overrun the width. End() hands the consumed height back to the column,
// so a wrapping run of buttons still stacks correctly with what follows.
type Flow struct {
	col      *Column
	x        float32
	h, gap   float32
	lines    int
	anyOnRow bool
}

func NewFlow(col *Column, h, gap float32) Flow {
	return Flow{col: col, x: col.X, h: h, gap: gap, lines: 1}
}

func (f *Flow) Next(w float32) rl.Rectangle {
	if f.anyOnRow && f.x+w > f.col.X+f.col.W {
		f.x = f.col.X
		f.lines++
	}
	r := rl.Rectangle{
		X: f.x, Y: f.col.Y + float32(f.lines-1)*(f.h+f.gap),
		Width: w, Height: f.h,
	}
	f.x += w + f.gap
	f.anyOnRow = true
	return r
}

func (f *Flow) End() {
	f.col.Y += float32(f.lines)*(f.h+f.gap) - f.gap
}

// Text draws at the rect's top-left — panels align rows on their top edge,
// not their centre line.
func Text(st *Style, r rl.Rectangle, s string, c rl.Color) {
	rl.DrawTextEx(st.Font, s, rl.Vector2{X: r.X, Y: r.Y}, st.FontSize, 1.0, c)
}

// TextCentered is what a chip or button label wants.
func TextCentered(st *Style, r rl.Rectangle, s string, c rl.Color) {
	size := rl.MeasureTextEx(st.Font, s, st.FontSize, 1)
	rl.DrawTextEx(st.Font, s, rl.Vector2{
		X: r.X + (r.Width-size.X)*0.5,
		Y: r.Y + (r.Height-size.Y)*0.5,
	}, st.FontSize, 1, c)
}

// TextRow writes one line into a fresh row-height band.
func TextRow(c *Column, st *Style, s string, col rl.Color) {
	Text(st, c.Band(st.RowH), s, col)
}

// TextClipped is Text that respects the rect's width, cutting the tail off
// with ".." when the string doesn't fit. ASCII only — the bundled font has no
// ellipsis glyph. Without it a long row just runs on under the scrollbar and
// off the panel, which reads as a rendering bug rather than as truncation.
func TextClipped(st *Style, r rl.Rectangle, s string, c rl.Color) {
	if s == "" || r.Width <= 0 {
		return
	}
	full := rl.MeasureTextEx(st.Font, s, st.FontSize, 1).X
	if full <= r.Width {
		Text(st, r, s, c)
		return
	}
	const tail = ".."
	// Proportional first guess: the bundled font is near-monospace, so this
	// lands within a character or two instead of walking the whole string.
	n := int(float32(len(s)) * r.Width / full)
	if n > len(s) {
		n = len(s)
	}
	for ; n > 0; n-- {
		cand := s[:n] + tail
		if rl.MeasureTextEx(st.Font, cand, st.FontSize, 1).X <= r.Width {
			Text(st, r, cand, c)
			return
		}
	}
	Text(st, r, tail, c)
}

// TextRowClipped is the Column flavour of TextClipped.
func TextRowClipped(c *Column, st *Style, s string, col rl.Color) {
	TextClipped(st, c.Band(st.RowH), s, col)
}

// Header is a section title in the style's header colour.
func Header(c *Column, st *Style, s string) { TextRow(c, st, s, st.Header) }

// Chip is the pill button every surface was reimplementing: filled rect,
// border, centred label, three background states. Returns true on click.
func Chip(in WidgetInput, st *Style, r rl.Rectangle, label string, active bool) bool {
	bg := st.Fill
	switch {
	case active:
		bg = st.FillActive
	case in.Hover(r):
		bg = st.FillHover
	}
	rl.DrawRectangleRec(r, bg)
	rl.DrawRectangleLinesEx(r, 1, st.Border)
	col := st.Text
	if active {
		col = contrastTextColor(bg)
	}
	TextCentered(st, r, label, col)
	return in.Clicked(r)
}

// ChipDisabled is the inert flavour: dim fill, dim label, no hover, no click.
func ChipDisabled(st *Style, r rl.Rectangle, label string) {
	rl.DrawRectangleRec(r, st.Disabled)
	rl.DrawRectangleLinesEx(r, 1, st.Border)
	TextCentered(st, r, label, st.TextDim)
}

// Toggle is a full-width chip carrying its own [X] / [ ] mark.
func Toggle(in WidgetInput, st *Style, r rl.Rectangle, label string, on bool) bool {
	bg := st.Fill
	if in.Hover(r) {
		bg = st.FillHover
	}
	rl.DrawRectangleRec(r, bg)
	rl.DrawRectangleLinesEx(r, 1, st.Border)
	mark := "[ ]"
	col := st.Text
	if on {
		mark = "[X]"
		col = st.FillActive
	}
	Text(st, rl.Rectangle{X: r.X + 4, Y: r.Y + 1}, mark+" "+label, col)
	return in.Clicked(r)
}

// Bar draws a proportional fill over a track. inset shrinks the fill on every
// side so a caller that also strokes a border doesn't paint over it. Colours
// are per-call: bars mean different things on different surfaces.
func Bar(r rl.Rectangle, ratio float32, track, fill rl.Color, inset float32) {
	rl.DrawRectangleRec(r, track)
	if ratio <= 0 {
		return
	}
	if ratio > 1 {
		ratio = 1
	}
	inner := rl.Rectangle{
		X: r.X + inset, Y: r.Y + inset,
		Width: (r.Width - 2*inset) * ratio, Height: r.Height - 2*inset,
	}
	if inner.Width > 0 && inner.Height > 0 {
		rl.DrawRectangleRec(inner, fill)
	}
}

// Scroller clips a panel's content, offsets layout by the stored scroll and
// reports the height consumed so the scrollbar can size itself next frame.
type Scroller struct {
	Col   Column
	state *ScrollState
	top   float32
	pad   float32
}

// BeginScroll opens a scissor; the caller must End() it.
func BeginScroll(content rl.Rectangle, s *ScrollState, padX, padY float32) Scroller {
	rl.BeginScissorMode(int32(content.X), int32(content.Y),
		int32(content.Width), int32(content.Height))
	offset := float32(0)
	if s != nil {
		offset = s.OffsetY
	}
	top := content.Y + padY
	return Scroller{
		Col: Column{
			X: content.X + padX,
			Y: top - offset,
			// Reserve the scrollbar track so content never draws under it.
			W:   content.Width - 2*padX - scrollbarTrackWidth,
			Gap: 0,
		},
		state: s,
		top:   top,
		pad:   padY,
	}
}

func (sc *Scroller) End() {
	if sc.state != nil {
		sc.state.ContentHeight = sc.Col.Y + sc.state.OffsetY - sc.top + sc.pad
	}
	rl.EndScissorMode()
}
