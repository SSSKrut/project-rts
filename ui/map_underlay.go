package ui

import (
	"math"

	rl "github.com/gen2brain/raylib-go/raylib"
)

// MapUnderlay is the pre-baked greyscale hill-shade texture rendered as the
// map's background. WorldOriginX/Z is the world position of the texture's
// (0,0) pixel; ScaleM is metres-per-pixel. The texture is GPU-resident; the
// image used to build it is freed in BakeUnderlay.
type MapUnderlay struct {
	Texture      rl.Texture2D
	WorldOriginX float32
	WorldOriginZ float32
	ScaleM       float32 // metres per pixel
	SizeM        float32 // total side length in metres
}

// HeightSampler is the procgen-height getter passed by main.go (a thin
// wrapper around systems.GroundHeight). Pulled out as a parameter to keep the
// ui package free of a systems import.
type HeightSampler func(worldX, worldZ float32) float32

// BakeUnderlay generates the hill-shade underlay for a square area
// `sizeM × sizeM` metres centred at (centerX, centerZ). Sample step is
// `scaleM` metres per pixel — 4 m/px for the 2 km Phase 10 placeholder
// (500×500 = 250 KB upload, sub-second bake on a modern CPU).
//
// Lighting model: diffuse only, light from above and to the NE at 45°. Slope
// dot product with the light direction gives the brightness.
func BakeUnderlay(centerX, centerZ, sizeM, scaleM float32, sample HeightSampler) MapUnderlay {
	if scaleM <= 0 {
		scaleM = 4
	}
	if sizeM < scaleM*4 {
		sizeM = scaleM * 4
	}
	pxSide := int32(math.Round(float64(sizeM / scaleM)))
	if pxSide < 4 {
		pxSide = 4
	}
	originX := centerX - sizeM*0.5
	originZ := centerZ - sizeM*0.5

	// Light: from (+0.5, +0.7, +0.5), normalised.
	lx, ly, lz := float32(0.5), float32(0.7), float32(0.5)
	mag := float32(math.Sqrt(float64(lx*lx + ly*ly + lz*lz)))
	lx /= mag
	ly /= mag
	lz /= mag

	// Fill a raw RGBA byte buffer ourselves — one CGo call per pixel via
	// ImageDrawPixel turns 500×500 into 250 k cross-language hops and takes
	// seconds at startup. NewImage + LoadTextureFromImage uploads the buffer
	// in one shot. Keep RGBA8 (not Grayscale) so the same code path can later
	// render coloured biome overlays without changing the texture format.
	buf := make([]byte, int(pxSide)*int(pxSide)*4)
	// Slope stencil: sample at ±slopeM rather than ±scaleM. The procgen
	// heightmap has high-frequency components that flip slope sign every few
	// metres; sampling at the pixel pitch produces a "noise mosaic" rather
	// than readable terrain. Stencil widened to 4× pixel pitch (16 m at
	// 4 m / px) smooths out the noise and reads as topography.
	slopeM := scaleM * 4
	for j := int32(0); j < pxSide; j++ {
		for i := int32(0); i < pxSide; i++ {
			wx := originX + float32(i)*scaleM
			wz := originZ + float32(j)*scaleM
			hxm := sample(wx-slopeM, wz)
			hxp := sample(wx+slopeM, wz)
			hzm := sample(wx, wz-slopeM)
			hzp := sample(wx, wz+slopeM)
			dhx := (hxp - hxm) / (2 * slopeM)
			dhz := (hzp - hzm) / (2 * slopeM)
			nx, ny, nz := -dhx, float32(1.0), -dhz
			nmag := float32(math.Sqrt(float64(nx*nx + ny*ny + nz*nz)))
			nx /= nmag
			ny /= nmag
			nz /= nmag
			lambert := nx*lx + ny*ly + nz*lz
			if lambert < 0 {
				lambert = 0
			}
			v := 30 + lambert*200
			if v > 255 {
				v = 255
			}
			idx := (int(j)*int(pxSide) + int(i)) * 4
			buf[idx+0] = uint8(v * 0.80)
			buf[idx+1] = uint8(v * 0.88)
			buf[idx+2] = uint8(v * 0.72)
			buf[idx+3] = 255
		}
	}
	// NewImage stores the Go-allocated `buf` pointer in the Image struct —
	// raylib's UnloadImage would C.free that pointer and crash. Skip
	// UnloadImage; once tex is uploaded the Go GC reclaims `buf` after this
	// function returns. (Same gotcha pattern as rl.UnloadModel on Go-allocated
	// meshes — see CLAUDE.md "raylib-go-gotchas".)
	img := rl.NewImage(buf, pxSide, pxSide, 1, rl.UncompressedR8g8b8a8)
	tex := rl.LoadTextureFromImage(img)
	rl.SetTextureFilter(tex, rl.FilterBilinear)

	return MapUnderlay{
		Texture:      tex,
		WorldOriginX: originX,
		WorldOriginZ: originZ,
		ScaleM:       scaleM,
		SizeM:        sizeM,
	}
}

// Unload releases the GPU texture. Pair with BakeUnderlay via defer in main.
func (u *MapUnderlay) Unload() {
	rl.UnloadTexture(u.Texture)
}
