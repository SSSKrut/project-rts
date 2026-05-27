package ui

import (
	"fmt"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
)

type ChipKind uint8

const (
	ChipKindLevel ChipKind = iota
	ChipKindWallMode
	ChipKindInside
)

// Index holds the level index (ChipKindLevel) or wall-mode enum value
// (ChipKindWallMode); ignored for Inside.
type WidgetChip struct {
	Rect   rl.Rectangle
	Kind   ChipKind
	Index  int
	Active bool
	Label  string
}

type BuildingWidgetLayout struct {
	Root  ecs.Entity
	Chips []WidgetChip
}

const (
	widgetChipW  = 36
	widgetChipH  = 22
	widgetGap    = 4
	widgetFontPx = 14
)

func ComputeBuildingWidget(root ecs.Entity, bvm *components.BuildingViewMode, levels []ecs.Entity, screenPos rl.Vector2, levelMap *ecs.Map[components.Level]) *BuildingWidgetLayout {
	if bvm == nil {
		return nil
	}

	out := &BuildingWidgetLayout{Root: root}

	rowX := screenPos.X + 24
	rowY := screenPos.Y - 78

	for i, lev := range levels {
		label := ""
		if levelMap != nil {
			if l := levelMap.Get(lev); l != nil {
				label = l.Name
			}
		}
		if label == "" {
			label = fmt.Sprintf("L%d", i)
		}
		out.Chips = append(out.Chips, WidgetChip{
			Rect: rl.Rectangle{
				X:      rowX + float32(i)*(widgetChipW+widgetGap),
				Y:      rowY,
				Width:  widgetChipW,
				Height: widgetChipH,
			},
			Kind:   ChipKindLevel,
			Index:  i,
			Active: lev == bvm.CurrentLevel,
			Label:  label,
		})
	}

	rowY2 := rowY + widgetChipH + widgetGap
	modeLabels := [3]string{"All", "Cam", "Wire"}
	for i, l := range modeLabels {
		out.Chips = append(out.Chips, WidgetChip{
			Rect: rl.Rectangle{
				X:      rowX + float32(i)*(widgetChipW+widgetGap),
				Y:      rowY2,
				Width:  widgetChipW,
				Height: widgetChipH,
			},
			Kind:   ChipKindWallMode,
			Index:  i,
			Active: components.WallRenderMode(i) == bvm.WallMode,
			Label:  l,
		})
	}

	rowY3 := rowY2 + widgetChipH + widgetGap
	insideLabel := "Closed"
	if bvm.InteriorOpen {
		insideLabel = "Open"
	}
	out.Chips = append(out.Chips, WidgetChip{
		Rect: rl.Rectangle{
			X:      rowX,
			Y:      rowY3,
			Width:  3*widgetChipW + 2*widgetGap,
			Height: widgetChipH,
		},
		Kind:   ChipKindInside,
		Active: bvm.InteriorOpen,
		Label:  insideLabel,
	})

	return out
}

func DrawBuildingWidget(layout *BuildingWidgetLayout, font rl.Font) {
	if layout == nil {
		return
	}
	bgIdle := rl.Color{R: 40, G: 44, B: 52, A: 220}
	bgActive := rl.Color{R: 100, G: 180, B: 220, A: 235}
	border := rl.Color{R: 20, G: 20, B: 20, A: 220}
	fg := rl.RayWhite

	for i := range layout.Chips {
		c := &layout.Chips[i]
		bg := bgIdle
		if c.Active {
			bg = bgActive
		}
		rl.DrawRectangleRec(c.Rect, bg)
		rl.DrawRectangleLinesEx(c.Rect, 1, border)
		ts := rl.MeasureTextEx(font, c.Label, widgetFontPx, 1)
		tx := c.Rect.X + (c.Rect.Width-ts.X)*0.5
		ty := c.Rect.Y + (c.Rect.Height-ts.Y)*0.5
		rl.DrawTextEx(font, c.Label, rl.Vector2{X: tx, Y: ty}, widgetFontPx, 1, fg)
	}
}

// HitTestBuildingWidget expects `mouse` in absolute window coords (the same
// space layout was built in), not panel-local.
func HitTestBuildingWidget(layout *BuildingWidgetLayout, mouse rl.Vector2) *WidgetChip {
	if layout == nil {
		return nil
	}
	for i := range layout.Chips {
		c := &layout.Chips[i]
		if mouse.X >= c.Rect.X && mouse.X <= c.Rect.X+c.Rect.Width &&
			mouse.Y >= c.Rect.Y && mouse.Y <= c.Rect.Y+c.Rect.Height {
			return c
		}
	}
	return nil
}
