package ui

import (
	"math"

	rl "github.com/gen2brain/raylib-go/raylib"
)

// MapUnderlay is the pre-baked greyscale hill-shade. WorldOriginX/Z is the
// world position of the texture's (0,0) pixel; ScaleM is metres-per-pixel.
type MapUnderlay struct {
	Texture      rl.Texture2D
	WorldOriginX float32
	WorldOriginZ float32
	ScaleM       float32
	SizeM        float32
}

// HeightSampler is injected to keep ui free of a systems import.
type HeightSampler func(worldX, worldZ float32) float32

// BakeUnderlay generates a hill-shade for a square `sizeM x sizeM` area.
// Lighting: diffuse only, light from above-NE at 45 deg.
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

	lx, ly, lz := float32(0.5), float32(0.7), float32(0.5)
	mag := float32(math.Sqrt(float64(lx*lx + ly*ly + lz*lz)))
	lx /= mag
	ly /= mag
	lz /= mag

	// Fill a raw RGBA buffer ourselves — ImageDrawPixel makes one CGo
	// hop per pixel, turning 500x500 into 250 k cross-language calls and
	// seconds at startup. NewImage + LoadTextureFromImage uploads it in
	// one shot. RGBA8 (not Grayscale) so coloured biome overlays slot in
	// without changing the texture format.
	buf := make([]byte, int(pxSide)*int(pxSide)*4)
	// Slope stencil widened to 4× pixel pitch smooths the procgen
	// high-frequency noise so the result reads as topography instead of
	// a noise mosaic.
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
	// raylib's UnloadImage would C.free the Go-allocated buf pointer
	// and crash. Skip UnloadImage; the Go GC reclaims buf once tex is
	// uploaded. Same gotcha as rl.UnloadModel on Go-allocated meshes.
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

func (u *MapUnderlay) Unload() {
	rl.UnloadTexture(u.Texture)
}
