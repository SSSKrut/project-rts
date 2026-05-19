package ui

import (
	rl "github.com/gen2brain/raylib-go/raylib"
)

// Scene3DRT owns the RenderTexture2D used to draw the 3D scene at the 3D
// panel's resolution. M10.2: keeping RT-size == panel-size minimises the
// upload area; the cost is a realloc whenever the panel size changes (Tab
// toggle or window resize). Both events are user-driven and rare, so the
// realloc cost is invisible.
type Scene3DRT struct {
	RT     rl.RenderTexture2D
	Width  int32
	Height int32
}

// NewScene3DRT allocates the initial render texture sized to the panel's
// current bounds. Caller must defer (*Scene3DRT).Unload() before window
// teardown.
func NewScene3DRT(panel Panel) *Scene3DRT {
	content := ContentRect(panel)
	w, h := contentSize(content)
	return &Scene3DRT{
		RT:     rl.LoadRenderTexture(w, h),
		Width:  w,
		Height: h,
	}
}

// EnsureSize reallocates the RT when the panel content rect changes size.
// No-op when current Width/Height already matches. Called after every
// PanelManager.Recompute or window-resize event.
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

// Unload frees the GPU resources. Always invoke via defer in main.go - the
// fbo + colour-texture pair leaks otherwise on shutdown.
func (s *Scene3DRT) Unload() {
	rl.UnloadRenderTexture(s.RT)
}

// Composite blits the RT into the panel's content rect. raylib renders RT
// textures bottom-up in OpenGL (Y flipped), so we pass a negative-height
// source rect to flip on draw. Drawn after EndTextureMode and before chrome.
func (s *Scene3DRT) Composite(panel Panel) {
	content := ContentRect(panel)
	src := rl.Rectangle{X: 0, Y: 0, Width: float32(s.Width), Height: -float32(s.Height)}
	dst := content
	rl.DrawTexturePro(s.RT.Texture, src, dst, rl.Vector2{}, 0, rl.White)
}

// contentSize clamps to a safe min - raylib's LoadRenderTexture rejects zero/
// negative dimensions. 1x1 fallback is enough for the brief moment when a
// panel collapses during preset toggle (shouldn't happen in L1 but cheap
// insurance).
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
