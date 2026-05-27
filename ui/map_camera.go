package ui

import (
	rl "github.com/gen2brain/raylib-go/raylib"

	"rts-go/components"
)

// MapCamera holds 2D map state (independent of the 3D camera). Zoom is
// pixels-per-metre.
type MapCamera struct {
	Center  components.WorldPos
	Zoom    float32
	MinZoom float32
	MaxZoom float32
}

func NewMapCamera() MapCamera {
	return MapCamera{
		Center:  components.WorldPos{},
		Zoom:    2.0,
		MinZoom: 0.25,
		MaxZoom: 8.0,
	}
}

// MapWorldToPanel returns absolute screen coordinates (not panel-local).
func MapWorldToPanel(wp components.WorldPos, cam MapCamera, panelBounds rl.Rectangle) rl.Vector2 {
	delta := wp.Sub(cam.Center)
	return rl.Vector2{
		X: panelBounds.X + panelBounds.Width*0.5 + delta.X*cam.Zoom,
		Y: panelBounds.Y + panelBounds.Height*0.5 + delta.Z*cam.Zoom,
	}
}

func MapPanelToWorld(screenPos rl.Vector2, cam MapCamera, panelBounds rl.Rectangle) components.WorldPos {
	dx := screenPos.X - (panelBounds.X + panelBounds.Width*0.5)
	dz := screenPos.Y - (panelBounds.Y + panelBounds.Height*0.5)
	return cam.Center.Add(rl.Vector3{X: dx / cam.Zoom, Z: dz / cam.Zoom})
}

// Pan subtracts because dragging the map right moves the centre left.
func (c *MapCamera) Pan(dxPx, dyPx float32) {
	c.Center = c.Center.Add(rl.Vector3{X: -dxPx / c.Zoom, Z: -dyPx / c.Zoom})
}

// ZoomAt keeps the world point under `pivot` fixed in screen space
// (standard CAD zoom-to-cursor).
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
	newScreen := MapWorldToPanel(worldUnderCursor, *c, panelBounds)
	c.Center = c.Center.Add(rl.Vector3{
		X: (newScreen.X - pivot.X) / c.Zoom,
		Z: (newScreen.Y - pivot.Y) / c.Zoom,
	})
}
