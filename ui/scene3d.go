package ui

import (
	rl "github.com/gen2brain/raylib-go/raylib"
)

// Scene3DRT owns the 3D scene's RenderTexture2D. Keeping RT-size ==
// panel-size minimises upload area; the realloc on resize is invisible
// because panel-resize events are rare.
type Scene3DRT struct {
	RT     rl.RenderTexture2D
	Width  int32
	Height int32
}

// NewScene3DRT: caller must defer Unload before window teardown.
func NewScene3DRT(panel Panel) *Scene3DRT {
	content := ContentRect(panel)
	w, h := contentSize(content)
	return &Scene3DRT{
		RT:     rl.LoadRenderTexture(w, h),
		Width:  w,
		Height: h,
	}
}

// EnsureSize is a no-op when the panel rect already matches.
func (s *Scene3DRT) EnsureSize(panel Panel) {
	content := ContentRect(panel)
	w, h := contentSize(content)
	if w == s.Width && h == s.Height {
		return
	}
	rl.UnloadRenderTexture(s.RT)
	s.RT = rl.LoadRenderTexture(w, h)
	s.Width = w
	s.Height = h
}

// Unload must run via defer; otherwise the fbo + colour-texture pair leaks.
func (s *Scene3DRT) Unload() {
	rl.UnloadRenderTexture(s.RT)
}

// Composite passes a negative-height source rect to flip the OpenGL
// bottom-up RT on draw. Must run after EndTextureMode and before chrome.
func (s *Scene3DRT) Composite(panel Panel) {
	content := ContentRect(panel)
	src := rl.Rectangle{X: 0, Y: 0, Width: float32(s.Width), Height: -float32(s.Height)}
	dst := content
	rl.DrawTexturePro(s.RT.Texture, src, dst, rl.Vector2{}, 0, rl.White)
}

// contentSize clamps to 1×1 because raylib's LoadRenderTexture rejects
// zero / negative dimensions.
func contentSize(r rl.Rectangle) (int32, int32) {
	w := int32(r.Width)
	h := int32(r.Height)
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	return w, h
}
