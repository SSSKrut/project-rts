package ui

import (
	"fmt"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
)

// The squad bar: one card per man in every squad the selection touches, laid
// out along the bottom of the 3D view. It answers "who is in my hands and who
// carries what" in a glance and lets the player pick the man with the right
// weapon in one click — the Inspector is for reading state, this is for
// commanding. Selection-bound and ephemeral, so it is an overlay rather than a
// workspace panel: no splitter ever fights it for space.

const (
	barCardW float32 = 56
	// A hull carries two weapon slots and a longer name, so it gets a wider
	// card rather than a cramped one — mixed squads read as mixed at a glance.
	barVehCardW  float32 = 88
	barCardH     float32 = 64
	barPad       float32 = 6
	barCardGap   float32 = 4
	barGroupGap  float32 = 14
	barChipW     float32 = 10
	barTombstone float32 = 5.0 // seconds a KIA card lingers
)

// BarHeight is what the bar occupies when visible; the host reserves it before
// any drawing happens so input can be gated in the same frame.
const BarHeight = barCardH + 2*barPad

var (
	barBG         = rl.Color{R: 14, G: 16, B: 20, A: 225}
	barCardBG     = rl.Color{R: 30, G: 34, B: 42, A: 255}
	barCardSel    = rl.Color{R: 40, G: 74, B: 104, A: 255}
	barCardHover  = rl.Color{R: 52, G: 60, B: 74, A: 255}
	barCardDead   = rl.Color{R: 34, G: 34, B: 36, A: 255}
	barBorder     = rl.Color{R: 12, G: 14, B: 18, A: 255}
	barSelEdge    = rl.Color{R: 120, G: 200, B: 235, A: 255}
	barThreat     = rl.Color{R: 235, G: 110, B: 80, A: 255}
	barReflexEdge = rl.Color{R: 240, G: 190, B: 90, A: 255}
	barPlanEdge   = rl.Color{R: 150, G: 205, B: 235, A: 255}
	barHPFill     = rl.Color{R: 90, G: 190, B: 90, A: 255}
	barHPLow      = rl.Color{R: 220, G: 70, B: 60, A: 255}
	barStamFill   = rl.Color{R: 210, G: 190, B: 70, A: 255}
	barTrack      = rl.Color{R: 22, G: 24, B: 30, A: 255}
)

// BarMember is one card's identity. Slot is the roster position it was last
// seen in; DeadAt > 0 marks a tombstone and holds the stamp it died at.
// Wide marks a hull, which needs a bigger card than a man.
type BarMember struct {
	Unit   ecs.Entity
	Slot   uint8
	DeadAt float32
	Wide   bool
}

func (m BarMember) width() float32 {
	if m.Wide {
		return barVehCardW
	}
	return barCardW
}

// BarGroup is a squad's worth of cards. Squad == zero entity is the soloist
// bucket — selected units that belong to no squad.
type BarGroup struct {
	Squad   ecs.Entity
	Members []BarMember
}

type BarCard struct {
	Rect  rl.Rectangle
	Unit  ecs.Entity
	Squad ecs.Entity
	Dead  bool
}

type BarGroupBox struct {
	Squad    ecs.Entity
	ChipRect rl.Rectangle
	Rect     rl.Rectangle
}

// BarLayout is what got placed. Used is the sub-rect actually covered: the
// host vetoes world clicks against it, so the empty tail of the strip still
// passes clicks through to the terrain.
type BarLayout struct {
	Cards  []BarCard
	Groups []BarGroupBox
	Used   rl.Rectangle
	Hidden int
}

// ComputeSquadBarLayout places groups left to right. Cards that would overrun
// the area are not placed and counted in Hidden — a caller that silently
// dropped them would read as "this is everyone" when it isn't.
func ComputeSquadBarLayout(groups []BarGroup, area rl.Rectangle) BarLayout {
	out := BarLayout{Used: rl.Rectangle{X: area.X, Y: area.Y, Height: area.Height}}
	x := area.X + barPad
	top := area.Y + barPad
	limit := area.X + area.Width - barPad

	for gi := range groups {
		g := &groups[gi]
		if len(g.Members) == 0 {
			continue
		}
		if gi > 0 {
			x += barGroupGap
		}
		groupStart := x
		chip := rl.Rectangle{X: x, Y: top, Width: barChipW, Height: barCardH}
		// The chip is the group's handle (click = select the whole squad), so
		// a group whose chip doesn't fit is entirely hidden, not half-drawn.
		if chip.X+chip.Width > limit {
			out.Hidden += len(g.Members)
			continue
		}
		x += barChipW + barCardGap

		placed := 0
		for _, m := range g.Members {
			w := m.width()
			if x+w > limit {
				out.Hidden += len(g.Members) - placed
				break
			}
			out.Cards = append(out.Cards, BarCard{
				Rect:  rl.Rectangle{X: x, Y: top, Width: w, Height: barCardH},
				Unit:  m.Unit,
				Squad: g.Squad,
				Dead:  m.DeadAt > 0,
			})
			x += w + barCardGap
			placed++
		}
		if placed == 0 {
			x = groupStart
			continue
		}
		x -= barCardGap
		out.Groups = append(out.Groups, BarGroupBox{
			Squad:    g.Squad,
			ChipRect: chip,
			Rect:     rl.Rectangle{X: groupStart, Y: top, Width: x - groupStart, Height: barCardH},
		})
	}
	if len(out.Cards) > 0 {
		out.Used.Width = x + barPad - area.X
	}
	return out
}

// SquadBarCtx borrows the Inspector's handle bundle — the bar reads the same
// components — plus pointer state and the frame clock.
type SquadBarCtx struct {
	InspectorMaps
	World      *ecs.World
	Selected   []ecs.Entity
	Hovered    ecs.Entity
	Font       rl.Font
	Cursor     rl.Vector2
	LMBPressed bool
	Shift      bool
	Now        float32
	SquadColor func(ent ecs.Entity) rl.Color

	st Style
	in WidgetInput
}

// DrawSquadBar renders a layout produced earlier in the frame. Splitting
// compute from draw is what lets the host veto world clicks over the bar
// before any input is handled.
func DrawSquadBar(layout BarLayout, ctx SquadBarCtx) {
	if len(layout.Cards) == 0 {
		return
	}
	ctx.st = InspectorStyle(ctx.Font)
	ctx.st.FontSize = 12
	ctx.in = WidgetInput{Cursor: ctx.Cursor, Press: ctx.LMBPressed, Enabled: true}

	rl.DrawRectangleRec(layout.Used, barBG)
	rl.DrawRectangleLinesEx(layout.Used, 1, barBorder)

	for _, g := range layout.Groups {
		colour := rl.Color{R: 80, G: 80, B: 80, A: 255}
		if ctx.SquadColor != nil && g.Squad != (ecs.Entity{}) {
			colour = ctx.SquadColor(g.Squad)
		}
		rl.DrawRectangleRec(g.ChipRect, colour)
		if ctx.in.Hover(g.ChipRect) {
			rl.DrawRectangleLinesEx(g.ChipRect, 2, barSelEdge)
		}
		// The chip is the way back out of a drill-down: one click re-selects
		// the whole squad without hunting for it in the 3D view.
		if g.Squad != (ecs.Entity{}) && ctx.in.Clicked(g.ChipRect) {
			SelectSquadRequest.Active = true
			SelectSquadRequest.Squad = g.Squad
		}
		// The brain's plan rides on the group box for the same reason a reflex
		// rides on a hull card: how the squad executes the order stopped being
		// the plain answer.
		if label := barPlanLabel(ctx, g.Squad); label != "" {
			lab := rl.Rectangle{X: g.Rect.X, Y: g.Rect.Y, Width: g.Rect.Width, Height: 13}
			rl.DrawRectangleRec(lab, rl.Color{R: 22, G: 19, B: 14, A: 235})
			TextClipped(&ctx.st, rl.Rectangle{X: lab.X + barChipW + 2, Y: lab.Y,
				Width: lab.Width - barChipW - 4, Height: lab.Height}, label, barPlanEdge)
		}
	}
	for _, c := range layout.Cards {
		drawBarCard(ctx, c)
	}
	if layout.Hidden > 0 {
		r := rl.Rectangle{X: layout.Used.X + layout.Used.Width - 4, Y: layout.Used.Y + barPad,
			Width: 30, Height: barCardH}
		Text(&ctx.st, r, fmt.Sprintf("+%d", layout.Hidden), ctx.st.TextDim)
	}
}

func drawBarCard(ctx SquadBarCtx, c BarCard) {
	alive := !c.Dead && ctx.World.Alive(c.Unit)
	bg := barCardBG
	switch {
	case !alive:
		bg = barCardDead
	case isSelected(ctx.Selected, c.Unit):
		bg = barCardSel
	case ctx.in.Hover(c.Rect) || ctx.Hovered == c.Unit:
		bg = barCardHover
	}
	rl.DrawRectangleRec(c.Rect, bg)

	if !alive {
		rl.DrawRectangleLinesEx(c.Rect, 1, barBorder)
		TextCentered(&ctx.st, c.Rect, "KIA", ctx.st.TextDim)
		return
	}

	// Identity band: role colour + short label, stance letter on the right.
	band := rl.Rectangle{X: c.Rect.X, Y: c.Rect.Y, Width: c.Rect.Width, Height: 16}
	label, tint := barIdentity(ctx, c.Unit)
	rl.DrawRectangleRec(band, tint)
	TextCentered(&ctx.st, rl.Rectangle{X: band.X, Y: band.Y, Width: band.Width - 12, Height: band.Height},
		label, contrastTextColor(tint))
	if st := ctx.StanceMap.Get(c.Unit); st != nil {
		name := stanceLabel(st.Code)
		if name != "" {
			TextCentered(&ctx.st, rl.Rectangle{X: band.X + band.Width - 12, Y: band.Y,
				Width: 12, Height: band.Height}, name[:1], contrastTextColor(tint))
		}
	}

	// The body is what the card exists to answer: what this thing is armed
	// with. One line for a man, one per barrel for a hull.
	body := rl.Rectangle{X: c.Rect.X + 3, Y: band.Y + band.Height + 2,
		Width: c.Rect.Width - 6, Height: 14}
	if isHull(ctx, c.Unit) {
		for i, w := range barVehicleWeapons(ctx, c.Unit) {
			row := body
			row.Y += float32(i) * 13
			TextClipped(&ctx.st, row, w, ctx.st.Text)
		}
	} else {
		TextClipped(&ctx.st, body, barWeaponLabel(ctx, c.Unit), ctx.st.Text)
	}

	drawBarMeter(ctx, c, 0, ctx.HPMap != nil, barHPRatio(ctx, c.Unit))
	drawBarMeter(ctx, c, 1, ctx.StaminaMap != nil, barStaminaRatio(ctx, c.Unit))

	if isSelected(ctx.Selected, c.Unit) {
		rl.DrawRectangleLinesEx(c.Rect, 2, barSelEdge)
	} else {
		rl.DrawRectangleLinesEx(c.Rect, 1, barBorder)
	}
	// Under fire blinks rather than tints: a static colour reads as another
	// role, a blink reads as "look here now".
	if th := ctx.ThreatMap.Get(c.Unit); th != nil && th.State >= components.ThreatThreatened {
		if int(ctx.Now*4)%2 == 0 {
			rl.DrawRectangleLinesEx(c.Rect, 2, barThreat)
		}
	}
	// A live reflex means the hull is driving itself; amber and labelled,
	// because what it is obeying right now is not the player's order.
	if kind := barReflex(ctx, c.Unit); kind != components.VehicleReflexNone {
		rl.DrawRectangleLinesEx(c.Rect, 2, barReflexEdge)
		// Backed strip: a tank's second barrel line sits right here, and the
		// reflex is the more urgent fact while it lasts.
		lab := rl.Rectangle{X: c.Rect.X + 1, Y: c.Rect.Y + c.Rect.Height - 34,
			Width: c.Rect.Width - 2, Height: 13}
		rl.DrawRectangleRec(lab, rl.Color{R: 22, G: 19, B: 14, A: 235})
		// Clipped, not centred: "Smoke and reverse" is wider than the card.
		TextClipped(&ctx.st, rl.Rectangle{X: lab.X + 2, Y: lab.Y,
			Width: lab.Width - 4, Height: lab.Height},
			components.VehicleReflexLabel(kind), barReflexEdge)
	}
	if ctx.in.Clicked(c.Rect) {
		SelectUnitRequest.Active = true
		SelectUnitRequest.Unit = c.Unit
		SelectUnitRequest.Additive = ctx.Shift
	}
}

// barPlanLabel names the squad brain's current mode, phase included for a
// ClearSeq so "Clearing" doesn't hide which step is running.
func barPlanLabel(ctx SquadBarCtx, squad ecs.Entity) string {
	if squad == (ecs.Entity{}) || ctx.SquadPlanMap == nil || !ctx.World.Alive(squad) {
		return ""
	}
	plan := ctx.SquadPlanMap.Get(squad)
	if plan == nil {
		return ""
	}
	label := components.SquadPlanLabel(plan.Mode)
	if label == "" {
		return ""
	}
	if plan.Mode == components.SquadPlanClearSeq {
		label += ": " + components.ClearPhaseLabel(plan.Phase)
	}
	return label
}

// drawBarMeter stacks the two thin bars at the card's foot; row 0 is HP.
func drawBarMeter(ctx SquadBarCtx, c BarCard, row int, present bool, ratio float32) {
	if !present || ratio < 0 {
		return
	}
	const h float32 = 5
	r := rl.Rectangle{
		X: c.Rect.X + 3, Y: c.Rect.Y + c.Rect.Height - 3 - float32(2-row)*(h+2),
		Width: c.Rect.Width - 6, Height: h,
	}
	fill := barStamFill
	if row == 0 {
		fill = barHPFill
		if ratio < 0.34 {
			fill = barHPLow
		}
	}
	Bar(r, ratio, barTrack, fill, 0)
}

// barIdentity is the card's headline: role for infantry, class for a hull.
func barIdentity(ctx SquadBarCtx, unit ecs.Entity) (string, rl.Color) {
	if ctx.VehicleMap != nil {
		if v := ctx.VehicleMap.Get(unit); v != nil {
			return components.SpecForVehicle(v.Kind).Name,
				rl.Color{R: 120, G: 125, B: 135, A: 255}
		}
	}
	role := components.RoleRifleman
	if ctx.RoleMap != nil {
		if r := ctx.RoleMap.Get(unit); r != nil {
			role = r.Kind
		}
	}
	return role.ShortLabel(), components.RoleColor(role)
}

// barVehicleWeapons lists the hull's barrels, best first — the loadout is the
// reason a hull card is wider than a man's.
func barVehicleWeapons(ctx SquadBarCtx, unit ecs.Entity) []string {
	eq := ctx.EquipmentMap.Get(unit)
	if eq == nil {
		return nil
	}
	out := make([]string, 0, 2)
	for _, w := range [2]ecs.Entity{eq.Primary, eq.Secondary} {
		if w == (ecs.Entity{}) || !ctx.World.Alive(w) {
			continue
		}
		if wc := ctx.WeaponMap.Get(w); wc != nil {
			out = append(out, fmt.Sprintf("%s %d", components.SpecForWeapon(wc.Kind).Name, wc.Ammo))
		}
	}
	return out
}

func barReflex(ctx SquadBarCtx, unit ecs.Entity) components.VehicleReflexKind {
	if ctx.VehicleOverrideMap == nil {
		return components.VehicleReflexNone
	}
	ov := ctx.VehicleOverrideMap.Get(unit)
	if ov == nil {
		return components.VehicleReflexNone
	}
	return ov.Kind
}

func barWeaponLabel(ctx SquadBarCtx, unit ecs.Entity) string {
	eq := ctx.EquipmentMap.Get(unit)
	if eq == nil || eq.Primary == (ecs.Entity{}) || !ctx.World.Alive(eq.Primary) {
		return "-"
	}
	w := ctx.WeaponMap.Get(eq.Primary)
	if w == nil {
		return "-"
	}
	return fmt.Sprintf("%s %d", components.SpecForWeapon(w.Kind).Name, w.Ammo)
}

// barHPRatio / barStaminaRatio return -1 when the component is absent, which
// hides the meter instead of drawing a misleading empty track.
func barHPRatio(ctx SquadBarCtx, unit ecs.Entity) float32 {
	if ctx.HPMap == nil {
		return -1
	}
	hp := ctx.HPMap.Get(unit)
	if hp == nil || hp.Max <= 0 {
		return -1
	}
	return hp.Current / hp.Max
}

func barStaminaRatio(ctx SquadBarCtx, unit ecs.Entity) float32 {
	if ctx.StaminaMap == nil {
		return -1
	}
	st := ctx.StaminaMap.Get(unit)
	if st == nil || st.MaxLevel <= 0 {
		return -1
	}
	return st.Current / st.MaxLevel
}

// SquadBarState is the bar's own memory: the composition it last showed, so a
// card keeps its place. It cannot be derived from the world — a dying unit
// leaves the roster in the same tick (DamageService calls SquadService.Leave),
// and cards that reshuffle under the cursor mid-firefight destroy the muscle
// memory the bar exists to build. UI-only; never saved.
type SquadBarState struct {
	groups map[ecs.Entity][]BarMember
	order  []ecs.Entity
}

func NewSquadBarState() *SquadBarState {
	return &SquadBarState{groups: map[ecs.Entity][]BarMember{}}
}

// Sync folds the world's current rosters into the remembered composition and
// returns what to draw. Scope is every squad the selection touches, plus one
// bucket for selected soloists.
func (s *SquadBarState) Sync(ctx SquadBarCtx) []BarGroup {
	if s.groups == nil {
		s.groups = map[ecs.Entity][]BarMember{}
	}
	live := s.scope(ctx)

	// Groups that dropped out of scope go immediately: the bar follows the
	// selection, and a stale squad is not a casualty to commemorate.
	for squad := range s.groups {
		if _, ok := live[squad]; !ok {
			delete(s.groups, squad)
		}
	}
	kept := s.order[:0]
	for _, squad := range s.order {
		if _, ok := live[squad]; ok {
			kept = append(kept, squad)
		}
	}
	s.order = kept
	added := 0
	for squad := range live {
		if _, known := s.groups[squad]; !known {
			s.order = append(s.order, squad)
			s.groups[squad] = nil
			added++
		}
	}
	// Map iteration is random, so freshly seen squads are sorted by id before
	// they join the order — otherwise two frames with the same selection can
	// disagree about who comes first.
	if added > 1 {
		sortEntities(s.order[len(s.order)-added:])
	}

	out := make([]BarGroup, 0, len(s.order))
	for _, squad := range s.order {
		s.groups[squad] = s.merge(ctx, s.groups[squad], live[squad])
		out = append(out, BarGroup{Squad: squad, Members: s.groups[squad]})
	}
	return out
}

// merge keeps remembered positions, stamps vanished members as tombstones,
// drops expired ones and appends newcomers at the end.
func (s *SquadBarState) merge(ctx SquadBarCtx, remembered []BarMember, current []BarMember) []BarMember {
	seen := make(map[ecs.Entity]bool, len(current))
	for _, m := range current {
		seen[m.Unit] = true
	}
	out := remembered[:0]
	for _, old := range remembered {
		switch {
		case seen[old.Unit]:
			for _, cur := range current {
				if cur.Unit == old.Unit {
					old.Slot = cur.Slot
					break
				}
			}
			old.DeadAt = 0
			out = append(out, old)
		case old.DeadAt == 0:
			old.DeadAt = ctx.Now
			out = append(out, old)
		case ctx.Now-old.DeadAt < barTombstone:
			out = append(out, old)
		}
	}
	known := make(map[ecs.Entity]bool, len(out))
	for _, m := range out {
		known[m.Unit] = true
	}
	for _, cur := range current {
		if !known[cur.Unit] {
			out = append(out, cur)
		}
	}
	return out
}

// scope resolves the selection into squads (full roster each) and soloists.
func (s *SquadBarState) scope(ctx SquadBarCtx) map[ecs.Entity][]BarMember {
	live := map[ecs.Entity][]BarMember{}
	var soloists []BarMember
	for _, sel := range ctx.Selected {
		if !ctx.World.Alive(sel) {
			continue
		}
		squad := ecs.Entity{}
		if ctx.SquadMemberMap != nil {
			if sm := ctx.SquadMemberMap.Get(sel); sm != nil {
				squad = sm.Squad
			}
		}
		if squad == (ecs.Entity{}) || !ctx.World.Alive(squad) {
			if !containsUnit(soloists, sel) {
				soloists = append(soloists, BarMember{
					Unit: sel, Slot: uint8(len(soloists)), Wide: isHull(ctx, sel),
				})
			}
			continue
		}
		if _, done := live[squad]; done {
			continue
		}
		live[squad] = rosterMembers(ctx, squad)
	}
	if len(soloists) > 0 {
		live[ecs.Entity{}] = soloists
	}
	return live
}

func rosterMembers(ctx SquadBarCtx, squad ecs.Entity) []BarMember {
	roster := ctx.RosterMap.Get(squad)
	if roster == nil {
		return nil
	}
	out := make([]BarMember, 0, roster.Count)
	for i := uint8(0); i < roster.Count; i++ {
		mem := roster.Members[i]
		if mem == (ecs.Entity{}) || !ctx.World.Alive(mem) {
			continue
		}
		out = append(out, BarMember{Unit: mem, Slot: i, Wide: isHull(ctx, mem)})
	}
	return out
}

func isHull(ctx SquadBarCtx, e ecs.Entity) bool {
	return ctx.VehicleMap != nil && ctx.VehicleMap.Has(e)
}

func containsUnit(list []BarMember, e ecs.Entity) bool {
	for _, m := range list {
		if m.Unit == e {
			return true
		}
	}
	return false
}

// sortEntities is an insertion sort by id — the slices here are a handful of
// squads, and it keeps the package free of a sort import for that.
func sortEntities(list []ecs.Entity) {
	for i := 1; i < len(list); i++ {
		for j := i; j > 0 && list[j].ID() < list[j-1].ID(); j-- {
			list[j], list[j-1] = list[j-1], list[j]
		}
	}
}
