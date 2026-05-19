package ui

import (
	rl "github.com/gen2brain/raylib-go/raylib"

	"rts-go/components"
)

// MapCamera is the 2D view state for the map panel (PHASE-10.md P5). No 3D
// projection - just a centre point and pixels-per-metre zoom. Pan and zoom
// are driven by main.go input handlers and stay independent of the 3D
// camera.
type MapCamera struct {
	// Center is the WorldPos pinned to the panel center. Pan moves this.
	Center components.WorldPos
	// Zoom is pixels-per-metre. Default 1.0 -> 1 m of world = 1 px on screen.
	Zoom    float32
	MinZoom float32
	MaxZoom float32
}

// NewMapCamera returns a camera centred at origin with default zoom range.
// 0.5 px/m (= 2 m/px) is a reasonable default for the 2x2 km test underlay.
func NewMapCamera() MapCamera {
	return MapCamera{
		Center:  components.WorldPos{},
		Zoom:    2.0,
		MinZoom: 0.25,
		MaxZoom: 8.0,
	}
}

// MapWorldToPanel projects a WorldPos onto the panel's screen-space rect.
// The result is in screen coordinates (not panel-local) - callers pass it
// directly to raylib draw calls.
func MapWorldToPanel(wp components.WorldPos, cam MapCamera, panelBounds rl.Rectangle) rl.Vector2 {
	delta := wp.Sub(cam.Center) // metres
	return rl.Vector2{
		X: panelBounds.X + panelBounds.Width*0.5 + delta.X*cam.Zoom,
		Y: panelBounds.Y + panelBounds.Height*0.5 + delta.Z*cam.Zoom,
	}
}

// MapPanelToWorld is the inverse - panel-screen-space pixel position back to
// a WorldPos. Used by map RMB / LMB to resolve the cursor target.
func MapPanelToWorld(screenPos rl.Vector2, cam MapCamera, panelBounds rl.Rectangle) components.WorldPos {
	dx := screenPos.X - (panelBounds.X + panelBounds.Width*0.5)
	dz := screenPos.Y - (panelBounds.Y + panelBounds.Height*0.5)
	return cam.Center.Add(rl.Vector3{X: dx / cam.Zoom, Z: dz / cam.Zoom})
}

// Pan adds (dxPanelPx, dyPanelPx) of cursor drag to the centre (subtract,
// because dragging the map right moves the centre left).
func (c *MapCamera) Pan(dxPx, dyPx float32) {
	c.Center = c.Center.Add(rl.Vector3{X: -dxPx / c.Zoom, Z: -dyPx / c.Zoom})
}

// ZoomAt applies a multiplicative zoom step centred on the screen-space
// `pivot`. Standard CAD pattern: the world point under the cursor must stay
// under the cursor after the zoom. Clamped to [MinZoom, MaxZoom].
func (c *MapCamera) ZoomAt(pivot rl.Vector2, panelBounds rl.Rectangle, factor float32) {
	old := c.Zoom
	newZoom := old * factor
	if newZoom < c.MinZoom {
		newZoom = c.MinZoom
	}
	if newZoom > c.MaxZoom {
		newZoom = c.MaxZoom
	}
	if newZoom == old {
		return
	}
	worldUnderCursor := MapPanelToWorld(pivot, *c, panelBounds)
	c.Zoom = newZoom
	// Adjust Center so worldUnderCursor maps to the same pivot pixel.
	newScreen := MapWorldToPanel(worldUnderCursor, *c, panelBounds)
	c.Center = c.Center.Add(rl.Vector3{
		X: (newScreen.X - pivot.X) / c.Zoom,
		Z: (newScreen.Y - pivot.Y) / c.Zoom,
	})
}
