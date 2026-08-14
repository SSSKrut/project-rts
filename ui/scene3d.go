package ui

import (
	rl "github.com/gen2brain/raylib-go/raylib"
)

// Scene3DRT owns the 3D scene's RenderTexture2D. Keeping RT-size ==
// panel-size minimises upload area; the realloc on resize is invisible
// because panel-resize events are rare.
//
// The RT is hand-rolled so its depth attachment is a sampleable TEXTURE (the
// cloud post-pass reads it); LoadRenderTexture only gives a renderbuffer.
// When the FBO fails to complete we fall back to the stock RT and DepthTex
// stays false — the cloud pass checks it and skips itself.
type Scene3DRT struct {
	RT       rl.RenderTexture2D
	Width    int32
	Height   int32
	DepthTex bool
}

// NewScene3DRT: caller must defer Unload before window teardown.
func NewScene3DRT(panel Panel) *Scene3DRT {
	content := ContentRect(panel)
	w, h := contentSize(content)
	s := &Scene3DRT{Width: w, Height: h}
	s.RT, s.DepthTex = loadSceneRT(w, h)
	return s
}

// EnsureSize is a no-op when the panel rect already matches.
func (s *Scene3DRT) EnsureSize(panel Panel) {
	content := ContentRect(panel)
	w, h := contentSize(content)
	if w == s.Width && h == s.Height {
		return
	}
	s.unloadRT()
	s.RT, s.DepthTex = loadSceneRT(w, h)
	s.Width = w
	s.Height = h
}

// Unload must run via defer; otherwise the fbo + colour-texture pair leaks.
func (s *Scene3DRT) Unload() {
	s.unloadRT()
}

func (s *Scene3DRT) unloadRT() {
	if !s.DepthTex {
		rl.UnloadRenderTexture(s.RT)
		return
	}
	// glDeleteTextures ignores already-deleted names, so the explicit depth
	// delete cannot double-free with whatever UnloadFramebuffer detaches.
	rl.UnloadTexture(s.RT.Texture)
	rl.UnloadTexture(s.RT.Depth)
	rl.UnloadFramebuffer(s.RT.ID)
}

func loadSceneRT(w, h int32) (rl.RenderTexture2D, bool) {
	fbo := rl.LoadFramebuffer()
	if fbo == 0 {
		return rl.LoadRenderTexture(w, h), false
	}
	img := rl.GenImageColor(int(w), int(h), rl.Blank)
	color := rl.LoadTextureFromImage(img)
	rl.UnloadImage(img)
	depth := rl.NewTexture2D(rl.LoadTextureDepth(w, h, false), w, h, 1, 0)
	rl.FramebufferAttach(fbo, color.ID, rl.AttachmentColorChannel0, rl.AttachmentTexture2d, 0)
	rl.FramebufferAttach(fbo, depth.ID, rl.AttachmentDepth, rl.AttachmentTexture2d, 0)
	if !rl.FramebufferComplete(fbo) {
		rl.UnloadTexture(color)
		rl.UnloadTexture(depth)
		rl.UnloadFramebuffer(fbo)
		return rl.LoadRenderTexture(w, h), false
	}
	return rl.NewRenderTexture2D(fbo, color, depth), true
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
