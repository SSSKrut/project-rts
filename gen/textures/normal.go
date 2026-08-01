// Package textures holds procedural texture generators. Same contract as the
// other gen/ packages: pure data in, pixel buffers out, no ECS and no GPU.
package textures

import (
	"math"
	"runtime"
	"sync"
)

// GroundParams describes one tileable ground surface. Size is the texture side
// in texels; the caller decides how many metres that covers, which is what
// makes every radius below meaningful — they are all in texels.
type GroundParams struct {
	Size int
	Seed int64

	Grain  []Octave    // fine sand / soil noise
	Gravel ScatterSpec // dense small chips
	Stones ScatterSpec // sparse larger stones
	Cracks CrackSpec
	Ripple RippleSpec

	// Strength scales the final slope; AOStrength scales the baked occlusion.
	Strength   float32
	AOStrength float32
	// Smooth blurs the height field before it is differentiated, in texels. It
	// is the difference between a surface that catches the light softly and
	// one that looks like cast concrete.
	Smooth int
	// MaskFromHeight writes the normalised height field into the variation
	// channel instead of fragment tint. A surface generated this way is a
	// blend mask, not a look: that is what the coarse companion is for.
	MaskFromHeight bool
}

// Octave is one band of the grain noise. Periods is how many times the band
// repeats across the tile, so it is always an integer and the tile stays
// seamless.
type Octave struct {
	Periods   int
	Amplitude float32
}

// ScatterSpec sprinkles fragments on a jittered lattice: one candidate per
// cell, Coverage of them surviving. Radius is in texels; Sharpness runs 0
// (domed pebble) to 1 (flat chip with a hard rim).
type ScatterSpec struct {
	Cells     int
	Coverage  float32
	RadiusMin float32
	RadiusMax float32
	Height    float32
	Sharpness float32
	// RimSoft fades the fragment out over the last of its radius. Without it
	// the profile meets the ground with a vertical tangent and the fragment
	// reads as a hard-edged chip; 0 keeps that, 0.4 buries it in the soil.
	RimSoft float32
	// Tint is how much a fragment lightens or darkens the surface, written to
	// the albedo-variation channel.
	Tint float32
}

// CrackSpec cuts Worley cell borders into the surface: dried-mud fissures.
// Width is in texels, measured on the border-distance field.
type CrackSpec struct {
	Cells int
	Width float32
	Depth float32
	// Fine adds a second, denser generation at this cell multiplier.
	Fine      int
	FineDepth float32
}

// RippleSpec lays parallel wind ridges across the tile — what makes sand read
// as sand. Periods is ridges per tile (integer, so it wraps); Warp bends them
// with low-frequency noise so they are not a sine grating.
type RippleSpec struct {
	Periods   int
	Amplitude float32
	Warp      float32
	WarpCells int
}

// DrySoilParams is baked earth: fine grain, dense gravel, sparse stones and a
// mud-crack network.
func DrySoilParams(size int, seed int64) GroundParams {
	// Radii assume ~100 texels per metre: gravel 2-5 cm, stones 7-16 cm.
	return GroundParams{
		Size: size,
		Seed: seed,
		Grain: []Octave{
			{Periods: 12, Amplitude: 0.30},
			{Periods: 28, Amplitude: 0.45},
			{Periods: 64, Amplitude: 0.30},
			{Periods: 140, Amplitude: 0.16},
		},
		Gravel: ScatterSpec{
			Cells: 150, Coverage: 0.55,
			RadiusMin: 1.6, RadiusMax: 4.5,
			Height: 0.55, Sharpness: 0.7, Tint: 0.22,
		},
		Stones: ScatterSpec{
			Cells: 44, Coverage: 0.30,
			RadiusMin: 4.5, RadiusMax: 9.0,
			Height: 1.0, Sharpness: 0.25, Tint: 0.38,
		},
		Cracks: CrackSpec{
			Cells: 13, Width: 5.5, Depth: 0.85,
			Fine: 3, FineDepth: 0.30,
		},
		Strength:   1.0,
		AOStrength: 1.0,
	}
}

// MeadowParams is soft turf: broad hummocks, the odd small stone, no cracks.
func MeadowParams(size int, seed int64) GroundParams {
	return GroundParams{
		Size: size,
		Seed: seed,
		Grain: []Octave{
			{Periods: 8, Amplitude: 0.8},
			{Periods: 18, Amplitude: 0.7},
			{Periods: 40, Amplitude: 0.3},
		},
		Gravel: ScatterSpec{
			Cells: 60, Coverage: 0.12,
			RadiusMin: 1.2, RadiusMax: 2.8,
			Height: 0.22, Sharpness: 0.2, Tint: 0.1, RimSoft: 0.45,
		},
		Strength:   0.45,
		AOStrength: 0.55,
		Smooth:     2,
	}
}

// GravelParams is a stone bed: two calibres packed edge to edge, no soil left
// between them.
func GravelParams(size int, seed int64) GroundParams {
	return GroundParams{
		Size: size,
		Seed: seed,
		Grain: []Octave{
			{Periods: 40, Amplitude: 0.5},
			{Periods: 110, Amplitude: 0.3},
		},
		Gravel: ScatterSpec{
			Cells: 190, Coverage: 0.85,
			RadiusMin: 1.5, RadiusMax: 3.6,
			Height: 0.7, Sharpness: 0.45, Tint: 0.3, RimSoft: 0.4,
		},
		Stones: ScatterSpec{
			Cells: 70, Coverage: 0.6,
			RadiusMin: 3.5, RadiusMax: 8.0,
			Height: 1.0, Sharpness: 0.25, Tint: 0.4, RimSoft: 0.35,
		},
		Strength:   0.6,
		AOStrength: 0.8,
		Smooth:     2,
	}
}

// SandParams is wind-rippled sand: ridges plus a very fine grain, nothing
// solid in it.
func SandParams(size int, seed int64) GroundParams {
	return GroundParams{
		Size: size,
		Seed: seed,
		Grain: []Octave{
			{Periods: 18, Amplitude: 0.35},
			{Periods: 44, Amplitude: 0.4},
			{Periods: 110, Amplitude: 0.35},
			{Periods: 220, Amplitude: 0.22},
		},
		Ripple: RippleSpec{
			Periods: 15, Amplitude: 0.18, Warp: 0.85, WarpCells: 3,
		},
		Strength:   0.42,
		AOStrength: 0.4,
		Smooth:     1,
	}
}

// SoftGroundParams is the coarse companion surface: broad, gentle undulation
// only. Stretched over tens of metres it gives the ground a form at working
// camera range without smearing gravel and cracks into a visible web.
func SoftGroundParams(size int, seed int64) GroundParams {
	return GroundParams{
		Size: size,
		Seed: seed,
		Grain: []Octave{
			{Periods: 3, Amplitude: 1.0},
			{Periods: 7, Amplitude: 0.55},
			{Periods: 15, Amplitude: 0.25},
		},
		Strength:       1.0,
		AOStrength:     0.5,
		MaskFromHeight: true,
	}
}

// forRows splits a row range across the CPUs. Every layer writes one row per
// iteration and reads only immutable inputs, so the split is deterministic.
func forRows(n int, fn func(y0, y1 int)) {
	workers := runtime.NumCPU()
	if workers > n {
		workers = n
	}
	if workers <= 1 {
		fn(0, n)
		return
	}
	var wg sync.WaitGroup
	span := (n + workers - 1) / workers
	for w := 0; w < workers; w++ {
		y0 := w * span
		if y0 >= n {
			break
		}
		y1 := y0 + span
		if y1 > n {
			y1 = n
		}
		wg.Add(1)
		go func(a, b int) {
			defer wg.Done()
			fn(a, b)
		}(y0, y1)
	}
	wg.Wait()
}

// Ground returns RGBA8 texels, row-major, ready for an OpenGL upload:
//
//	R,G — tangent-space slope, [-1,1] encoded into [0,255]
//	B   — baked ambient occlusion (255 = open)
//	A   — albedo variation (128 = neutral)
//
// Seamless in both axes: every layer wraps on the tile.
func Ground(p GroundParams) []uint8 {
	n := p.Size
	h, tint := groundFields(p)

	if p.Smooth > 0 {
		h = boxBlurWrap(h, n, p.Smooth)
	}

	out := make([]uint8, n*n*4)
	ao := occlusion(h, n, p.AOStrength)
	if p.MaskFromHeight {
		tint = normalized(h)
	}
	// One texel is 1/n of the tile in either direction, so this keeps Strength
	// independent of resolution.
	scale := p.Strength * float32(n) * 0.5
	forRows(n, func(y0, y1 int) {
		for y := y0; y < y1; y++ {
			for x := 0; x < n; x++ {
				hl := h[y*n+wrap(x-1, n)]
				hr := h[y*n+wrap(x+1, n)]
				hd := h[wrap(y-1, n)*n+x]
				hu := h[wrap(y+1, n)*n+x]

				nx := -(hr - hl) * scale
				ny := -(hu - hd) * scale
				inv := 1 / float32(math.Sqrt(float64(nx*nx+ny*ny+1)))

				i := (y*n + x) * 4
				out[i] = encode(nx * inv)
				out[i+1] = encode(ny * inv)
				out[i+2] = quantize(ao[y*n+x])
				out[i+3] = encode(tint[y*n+x])
			}
		}
	})
	return out
}

// normalized rescales a field to [-1,1] so it can ride the variation channel.
func normalized(src []float32) []float32 {
	lo, hi := float32(math.MaxFloat32), float32(-math.MaxFloat32)
	for _, v := range src {
		if v < lo {
			lo = v
		}
		if v > hi {
			hi = v
		}
	}
	out := make([]float32, len(src))
	span := hi - lo
	if span <= 0 {
		return out
	}
	for i, v := range src {
		out[i] = (v-lo)/span*2 - 1
	}
	return out
}

// groundFields builds the height field and its albedo-variation companion.
func groundFields(p GroundParams) (height, tint []float32) {
	n := p.Size
	height = make([]float32, n*n)
	tint = make([]float32, n*n)

	var grainTotal float32
	for _, o := range p.Grain {
		grainTotal += o.Amplitude
	}
	if grainTotal > 0 {
		for oi, o := range p.Grain {
			addOctave(height, n, p.Seed, int32(oi), o, 0.35*o.Amplitude/grainTotal)
		}
	}

	addRipple(height, n, p.Seed, p.Ripple)
	carveCracks(height, n, p.Seed, p.Cracks)
	scatter(height, tint, n, p.Seed, 71, p.Gravel)
	scatter(height, tint, n, p.Seed, 137, p.Stones)
	return height, tint
}

// occlusion approximates AO by how far a texel sits below its neighbourhood:
// crack floors and stone contact shadows darken, stone caps stay open.
func occlusion(h []float32, n int, strength float32) []float32 {
	const radius = 5
	blur := boxBlurWrap(h, n, radius)
	ao := make([]float32, n*n)
	for i := range h {
		d := (h[i] - blur[i]) * 3 * strength
		v := 1 + d
		if v < 0.25 {
			v = 0.25
		} else if v > 1 {
			v = 1
		}
		ao[i] = v
	}
	return ao
}

// boxBlurWrap is a separable box blur on a torus.
func boxBlurWrap(src []float32, n, r int) []float32 {
	tmp := make([]float32, n*n)
	dst := make([]float32, n*n)
	inv := 1 / float32(2*r+1)
	forRows(n, func(y0, y1 int) {
		for y := y0; y < y1; y++ {
			for x := 0; x < n; x++ {
				var sum float32
				for k := -r; k <= r; k++ {
					sum += src[y*n+wrap(x+k, n)]
				}
				tmp[y*n+x] = sum * inv
			}
		}
	})
	forRows(n, func(y0, y1 int) {
		for y := y0; y < y1; y++ {
			for x := 0; x < n; x++ {
				var sum float32
				for k := -r; k <= r; k++ {
					sum += tmp[wrap(y+k, n)*n+x]
				}
				dst[y*n+x] = sum * inv
			}
		}
	})
	return dst
}

// scatter drops rounded fragments on a jittered lattice: one candidate per
// cell, positions absolute so torusDelta handles the wrap. Only cells within
// reach of a texel are consulted, so cost is bounded by the lattice.
func scatter(height, tint []float32, n int, seed int64, salt int32, s ScatterSpec) {
	if s.Cells <= 0 || s.Height == 0 {
		return
	}
	cell := float32(n) / float32(s.Cells)
	reach := int(math.Ceil(float64(s.RadiusMax/cell))) + 1

	type frag struct {
		cx, cy, r, h, t float32
		live            bool
	}
	frags := make([]frag, s.Cells*s.Cells)
	for cj := 0; cj < s.Cells; cj++ {
		for ci := 0; ci < s.Cells; ci++ {
			f := &frags[cj*s.Cells+ci]
			if hash01(seed, salt, int32(ci), int32(cj), 0) > s.Coverage {
				continue
			}
			f.live = true
			f.cx = (float32(ci) + hash01(seed, salt, int32(ci), int32(cj), 1)) * cell
			f.cy = (float32(cj) + hash01(seed, salt, int32(ci), int32(cj), 2)) * cell
			f.r = s.RadiusMin + hash01(seed, salt, int32(ci), int32(cj), 3)*(s.RadiusMax-s.RadiusMin)
			f.h = s.Height * (0.6 + 0.4*hash01(seed, salt, int32(ci), int32(cj), 4))
			f.t = (hash01(seed, salt, int32(ci), int32(cj), 5)*2 - 1) * s.Tint
		}
	}

	forRows(n, func(y0, y1 int) {
		for y := y0; y < y1; y++ {
			cj := int(float32(y) / cell)
			for x := 0; x < n; x++ {
				ci := int(float32(x) / cell)
				var addH, addT, best float32
				for dj := -reach; dj <= reach; dj++ {
					for di := -reach; di <= reach; di++ {
						f := &frags[wrap(cj+dj, s.Cells)*s.Cells+wrap(ci+di, s.Cells)]
						if !f.live {
							continue
						}
						dx := torusDelta(float32(x), f.cx, float32(n))
						dy := torusDelta(float32(y), f.cy, float32(n))
						d2 := dx*dx + dy*dy
						if d2 >= f.r*f.r {
							continue
						}
						t := 1 - float32(math.Sqrt(float64(d2)))/f.r
						dome := domeProfile(t, s.Sharpness, s.RimSoft)
						if dome*f.h > addH {
							addH = dome * f.h
						}
						if dome > best {
							best = dome
							addT = f.t
						}
					}
				}
				height[y*n+x] += addH
				tint[y*n+x] += addT
			}
		}
	})
}

// domeProfile shapes a fragment: 1 at the centre, 0 at the rim. Shape 0 is a
// domed hemisphere, 1 a flat plate with a hard rim. Cheaper than a pow and
// smooth in between, which is all the surface needs.
func domeProfile(t, shape, rimSoft float32) float32 {
	if t <= 0 {
		return 0
	}
	round := float32(math.Sqrt(float64(t)))
	flat := t * t
	var v float32
	switch {
	case shape <= 0:
		v = round
	case shape >= 1:
		v = flat
	default:
		v = round + (flat-round)*shape
	}
	if rimSoft > 0 && t < rimSoft {
		v *= smoothstep(t / rimSoft)
	}
	return v
}

// addRipple lays warped parallel ridges along X.
func addRipple(dst []float32, n int, seed int64, r RippleSpec) {
	if r.Periods <= 0 || r.Amplitude == 0 {
		return
	}
	warpCells := r.WarpCells
	if warpCells <= 0 {
		warpCells = 4
	}
	warp := make([]float32, n*n)
	addOctave(warp, n, seed, 191, Octave{Periods: warpCells, Amplitude: 1}, 1)

	k := 2 * math.Pi * float64(r.Periods) / float64(n)
	forRows(n, func(y0, y1 int) {
		for y := y0; y < y1; y++ {
			for x := 0; x < n; x++ {
				phase := k*float64(x) + 2*math.Pi*float64(r.Warp*warp[y*n+x])
				dst[y*n+x] += r.Amplitude * float32(math.Sin(phase))
			}
		}
	})
}

// carveCracks cuts the borders of a Worley lattice into the surface.
func carveCracks(height []float32, n int, seed int64, c CrackSpec) {
	if c.Cells <= 0 || c.Depth == 0 {
		return
	}
	cutOne(height, n, seed, 211, c.Cells, c.Width, c.Depth)
	if c.Fine > 1 && c.FineDepth != 0 {
		cutOne(height, n, seed, 223, c.Cells*c.Fine, c.Width*0.5, c.FineDepth)
	}
}

func cutOne(height []float32, n int, seed int64, salt int32, cells int, width, depth float32) {
	cell := float32(n) / float32(cells)
	sites := make([][2]float32, cells*cells)
	for cj := 0; cj < cells; cj++ {
		for ci := 0; ci < cells; ci++ {
			sites[cj*cells+ci] = [2]float32{
				(float32(ci) + hash01(seed, salt, int32(ci), int32(cj), 0)) * cell,
				(float32(cj) + hash01(seed, salt, int32(ci), int32(cj), 1)) * cell,
			}
		}
	}
	forRows(n, func(y0, y1 int) {
		for y := y0; y < y1; y++ {
			cj := int(float32(y) / cell)
			for x := 0; x < n; x++ {
				ci := int(float32(x) / cell)
				// F1 and F2 over the 5x5 neighbourhood; their difference is the
				// distance to the cell border, which is where a crack runs.
				f1, f2 := float32(math.MaxFloat32), float32(math.MaxFloat32)
				for dj := -2; dj <= 2; dj++ {
					for di := -2; di <= 2; di++ {
						s := sites[wrap(cj+dj, cells)*cells+wrap(ci+di, cells)]
						dx := torusDelta(float32(x), s[0], float32(n))
						dy := torusDelta(float32(y), s[1], float32(n))
						d := dx*dx + dy*dy
						if d < f1 {
							f1, f2 = d, f1
						} else if d < f2 {
							f2 = d
						}
					}
				}
				border := float32(math.Sqrt(float64(f2))) - float32(math.Sqrt(float64(f1)))
				if border >= width {
					continue
				}
				t := 1 - border/width
				height[y*n+x] -= depth * t * t
			}
		}
	})
}

// torusDelta is the shortest signed distance between two coordinates on a
// wrapping axis.
func torusDelta(a, b, n float32) float32 {
	d := a - b
	if d > n*0.5 {
		d -= n
	} else if d < -n*0.5 {
		d += n
	}
	return d
}

func encode(v float32) uint8 {
	e := (v*0.5 + 0.5) * 255
	if e < 0 {
		return 0
	}
	if e > 255 {
		return 255
	}
	return uint8(e)
}

func quantize(v float32) uint8 {
	e := v * 255
	if e < 0 {
		return 0
	}
	if e > 255 {
		return 255
	}
	return uint8(e)
}

// addOctave accumulates one band of smoothstep-interpolated value noise on a
// Periods×Periods lattice that wraps at the tile edge.
func addOctave(dst []float32, n int, seed int64, salt int32, o Octave, weight float32) {
	if o.Periods <= 0 {
		return
	}
	cell := float32(n) / float32(o.Periods)
	forRows(n, func(y0, y1 int) {
		for y := y0; y < y1; y++ {
			gy := float32(y) / cell
			j := int(gy)
			fy := smoothstep(gy - float32(j))
			for x := 0; x < n; x++ {
				gx := float32(x) / cell
				i := int(gx)
				fx := smoothstep(gx - float32(i))

				v00 := lattice(seed, salt, i, j, o.Periods)
				v10 := lattice(seed, salt, i+1, j, o.Periods)
				v01 := lattice(seed, salt, i, j+1, o.Periods)
				v11 := lattice(seed, salt, i+1, j+1, o.Periods)

				top := v00 + (v10-v00)*fx
				bot := v01 + (v11-v01)*fx
				dst[y*n+x] += (top + (bot-top)*fy) * weight
			}
		}
	})
}

func lattice(seed int64, salt int32, i, j, periods int) float32 {
	return hash01(seed, salt, int32(wrap(i, periods)), int32(wrap(j, periods)), 9)*2 - 1
}

func hash01(seed int64, args ...int32) float32 {
	h := uint64(seed)
	for _, a := range args {
		h ^= uint64(uint32(a))
		h ^= h >> 30
		h *= 0xbf58476d1ce4e5b9
		h ^= h >> 27
		h *= 0x94d049bb133111eb
		h ^= h >> 31
	}
	return float32(h>>40) / float32(1<<24)
}

func smoothstep(t float32) float32 { return t * t * (3 - 2*t) }

func wrap(v, n int) int {
	v %= n
	if v < 0 {
		v += n
	}
	return v
}
