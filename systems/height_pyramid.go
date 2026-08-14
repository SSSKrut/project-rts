package systems

// HeightPyramid is the resident coarse-height view of the operational area:
// level 0 is sampled from the terrain source once at boot, every next level is
// a box-filtered downsample (direct coarse sampling of fBm would alias the
// fine octaves). Far-terrain rings and any other "how high is the ground,
// roughly" consumer read this instead of the generation function, so swapping
// procgen for a real-planet DEM later only replaces the bake: real datasets
// ship exactly this pyramid as overview levels.
type PyramidLevel struct {
	Step float32 // metres per node
	Min  float32 // world X/Z of node 0 (square coverage, same both axes)
	N    int     // nodes per side
	H    []float32
}

type HeightPyramid struct {
	Levels []PyramidLevel
}

// BakeHeightPyramid covers [-half, +half]² with a baseStep grid, then adds
// `extraLevels` downsampled levels, each 4× coarser than the previous.
func BakeHeightPyramid(half, baseStep float32, extraLevels int, fn func(wx, wz float32) float32) *HeightPyramid {
	n := int(2*half/baseStep) + 1
	base := PyramidLevel{Step: baseStep, Min: -half, N: n, H: make([]float32, n*n)}
	for j := 0; j < n; j++ {
		wz := base.Min + float32(j)*baseStep
		for i := 0; i < n; i++ {
			base.H[j*n+i] = fn(base.Min+float32(i)*baseStep, wz)
		}
	}
	p := &HeightPyramid{Levels: []PyramidLevel{base}}
	for l := 0; l < extraLevels; l++ {
		p.Levels = append(p.Levels, downsample4(p.Levels[len(p.Levels)-1]))
	}
	return p
}

// downsample4 box-filters a level down to quarter resolution (5×5 window
// around every kept node, clamped at the borders).
func downsample4(src PyramidLevel) PyramidLevel {
	n := (src.N-1)/4 + 1
	dst := PyramidLevel{Step: src.Step * 4, Min: src.Min, N: n, H: make([]float32, n*n)}
	clamp := func(v int) int {
		if v < 0 {
			return 0
		}
		if v >= src.N {
			return src.N - 1
		}
		return v
	}
	for j := 0; j < n; j++ {
		for i := 0; i < n; i++ {
			var sum float32
			for dj := -2; dj <= 2; dj++ {
				sj := clamp(j*4 + dj)
				for di := -2; di <= 2; di++ {
					sum += src.H[sj*src.N+clamp(i*4+di)]
				}
			}
			dst.H[j*n+i] = sum / 25
		}
	}
	return dst
}

// Sample bilinearly interpolates a level at a world point, clamped to the
// coverage — outside the operational area the terrain freezes at its border
// values and the horizon haze owns the rest.
func (p *HeightPyramid) Sample(level int, wx, wz float32) float32 {
	if level < 0 {
		level = 0
	}
	if level >= len(p.Levels) {
		level = len(p.Levels) - 1
	}
	l := &p.Levels[level]
	fx := (wx - l.Min) / l.Step
	fz := (wz - l.Min) / l.Step
	maxIdx := float32(l.N - 1)
	if fx < 0 {
		fx = 0
	} else if fx > maxIdx {
		fx = maxIdx
	}
	if fz < 0 {
		fz = 0
	} else if fz > maxIdx {
		fz = maxIdx
	}
	i0 := int(fx)
	j0 := int(fz)
	if i0 >= l.N-1 {
		i0 = l.N - 2
	}
	if j0 >= l.N-1 {
		j0 = l.N - 2
	}
	tx := fx - float32(i0)
	tz := fz - float32(j0)
	h00 := l.H[j0*l.N+i0]
	h10 := l.H[j0*l.N+i0+1]
	h01 := l.H[(j0+1)*l.N+i0]
	h11 := l.H[(j0+1)*l.N+i0+1]
	return h00*(1-tx)*(1-tz) + h10*tx*(1-tz) + h01*(1-tx)*tz + h11*tx*tz
}
