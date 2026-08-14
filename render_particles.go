package main

import (
	"math"
	"sort"

	rl "github.com/gen2brain/raylib-go/raylib"

	"rts-go/systems"
)

// Billboard smoke pass: every smoke/dust puff and SmokeField cluster renders
// as a camera-facing quad through one shader that shares the cloud pass's
// visual language — same pair-slice noise for erosion, same sun for the fake
// sphere lighting. Depth test on / depth write off, back-to-front sorted, so
// puffs blend over the scene without cutting hard edges into it; the part of
// a quad that dips below terrain fades out at the vertex level instead of
// z-clipping.
const particleFS = `#version 330
in vec2 fragTexCoord;
in vec4 fragColor;

uniform sampler2D texture0;   // pair-slice value noise (cloud texture)
uniform vec3 uSunCam;         // sun dir in camera basis (right, up, toward-cam)
uniform vec3 uSunColor;
uniform vec3 uAmbient;
uniform float uTime;

out vec4 finalColor;

float n2(vec2 p) { return texture(texture0, p).y; }
float fbm2(vec2 p) {
    return (0.5 * n2(p) + 0.25 * n2(p * 2.03 + 17.17) + 0.125 * n2(p * 4.01 + 31.31)) / 0.875;
}

void main() {
    vec2 corner = fract(fragTexCoord);
    vec2 c = corner * 2.0 - 1.0;
    float r2 = dot(c, c);
    if (r2 > 1.0) discard;

    // Integer part of the UV is the per-particle noise-cell offset.
    vec2 np = fragTexCoord * 0.055 + vec2(uTime * 0.006, -uTime * 0.009);
    float n = fbm2(np);

    // Noise warps the radius (ragged silhouette), then density = body x noise;
    // the threshold rises as the particle fades, so old smoke erodes from its
    // thin regions instead of dimming uniformly — one alpha channel drives both.
    float fade = fragColor.a;
    float rr = r2 * (0.65 + 0.95 * n);
    float body = smoothstep(1.0, 0.2, rr);
    float d = body * (0.5 + 0.75 * n);
    float th = (1.0 - fade) * 0.6;
    float a = smoothstep(th, th + 0.35, d) * fade * 0.85;
    if (a < 0.004) discard;

    // Fake sphere normal from the quad corner; wrap lighting keeps the shaded
    // side from going black — smoke scatters, it doesn't diffuse-shade.
    vec3 nrm = normalize(vec3(c.x, c.y, sqrt(max(1.0 - r2, 0.05))));
    float diff = clamp(dot(nrm, uSunCam) * 0.5 + 0.55, 0.0, 1.0);
    vec3 col = fragColor.rgb * (uAmbient + uSunColor * diff * 0.95);
    finalColor = vec4(col, a);
}
`

type puffDraw struct {
	pos     rl.Vector3
	size    float32
	tint    rl.Color
	fade    float32
	seed    uint32
	groundY float32
	distSq  float32
}

type particleRenderer struct {
	shader   rl.Shader
	noise    rl.Texture2D
	locSun   int32
	locColor int32
	locAmb   int32
	locTime  int32
	lightDir [3]float32
	lightCol [3]float32
	ambient  [3]float32
	batch    []puffDraw
	ok       bool
}

func newParticleRenderer() *particleRenderer {
	p := &particleRenderer{batch: make([]puffDraw, 0, 512)}
	p.shader = rl.LoadShaderFromMemory(cloudVS, particleFS)
	if p.shader.ID == 0 {
		return p
	}
	p.noise = uploadCloudNoise()
	p.locSun = rl.GetShaderLocation(p.shader, "uSunCam")
	p.locColor = rl.GetShaderLocation(p.shader, "uSunColor")
	p.locAmb = rl.GetShaderLocation(p.shader, "uAmbient")
	p.locTime = rl.GetShaderLocation(p.shader, "uTime")
	p.lightDir = normalize3(sunDir)
	p.lightCol = sunColor
	p.ambient = [3]float32{0.50, 0.52, 0.57}
	p.ok = true
	return p
}

// setLight hands the frame's daylight palette to the pass; puffs share the
// scene's sun and the cloud layer's base ambient.
func (p *particleRenderer) setLight(pal *skyPalette) {
	p.lightDir = pal.lightDir
	p.lightCol = pal.lightCol
	p.ambient = pal.cloudAmbB
}

func (p *particleRenderer) add(d puffDraw) {
	cam := systems.CurrentCamera.Position
	dx := d.pos.X - cam.X
	dy := d.pos.Y - cam.Y
	dz := d.pos.Z - cam.Z
	d.distSq = dx*dx + dy*dy + dz*dz
	p.batch = append(p.batch, d)
}

// flush draws the collected puffs back-to-front and clears the batch. Must run
// inside BeginMode3D, after every opaque draw.
func (p *particleRenderer) flush() {
	if !p.ok || len(p.batch) == 0 {
		p.batch = p.batch[:0]
		return
	}
	sort.Slice(p.batch, func(i, j int) bool { return p.batch[i].distSq > p.batch[j].distSq })

	cam := systems.CurrentCamera
	fwd := vecNorm(rl.Vector3{
		X: cam.Target.X - cam.Position.X,
		Y: cam.Target.Y - cam.Position.Y,
		Z: cam.Target.Z - cam.Position.Z,
	})
	right := vecNorm(vecCross(fwd, cam.Up))
	up := vecCross(right, fwd)

	sun := p.lightDir
	sunCam := []float32{
		sun[0]*right.X + sun[1]*right.Y + sun[2]*right.Z,
		sun[0]*up.X + sun[1]*up.Y + sun[2]*up.Z,
		-(sun[0]*fwd.X + sun[1]*fwd.Y + sun[2]*fwd.Z),
	}
	rl.SetShaderValue(p.shader, p.locSun, sunCam, rl.ShaderUniformVec3)
	setVec3(p.shader, p.locColor, p.lightCol)
	setVec3(p.shader, p.locAmb, p.ambient)
	rl.SetShaderValue(p.shader, p.locTime, []float32{float32(rl.GetTime())}, rl.ShaderUniformFloat)

	rl.BeginShaderMode(p.shader)
	rl.DisableDepthMask()
	rl.SetTexture(p.noise.ID)

	for i := range p.batch {
		d := &p.batch[i]
		s := d.size
		if s <= 0.01 {
			continue
		}
		ou := float32(d.seed & 15)
		ov := float32((d.seed >> 4) & 15)
		fadeH := s
		if fadeH < 0.8 {
			fadeH = 0.8
		}
		vtxAlpha := func(y float32) uint8 {
			k := (y-d.groundY)/fadeH + 1.0
			if k <= 0 {
				return 0
			}
			if k > 1 {
				k = 1
			}
			return uint8(d.fade * k * 255)
		}
		bl := rl.Vector3{X: d.pos.X - right.X*s - up.X*s, Y: d.pos.Y - right.Y*s - up.Y*s, Z: d.pos.Z - right.Z*s - up.Z*s}
		br := rl.Vector3{X: d.pos.X + right.X*s - up.X*s, Y: d.pos.Y + right.Y*s - up.Y*s, Z: d.pos.Z + right.Z*s - up.Z*s}
		tr := rl.Vector3{X: d.pos.X + right.X*s + up.X*s, Y: d.pos.Y + right.Y*s + up.Y*s, Z: d.pos.Z + right.Z*s + up.Z*s}
		tl := rl.Vector3{X: d.pos.X - right.X*s + up.X*s, Y: d.pos.Y - right.Y*s + up.Y*s, Z: d.pos.Z - right.Z*s + up.Z*s}

		rl.CheckRenderBatchLimit(4)
		rl.Begin(rl.Quads)
		rl.Color4ub(d.tint.R, d.tint.G, d.tint.B, vtxAlpha(bl.Y))
		rl.TexCoord2f(ou, ov)
		rl.Vertex3f(bl.X, bl.Y, bl.Z)
		rl.Color4ub(d.tint.R, d.tint.G, d.tint.B, vtxAlpha(br.Y))
		rl.TexCoord2f(ou+1, ov)
		rl.Vertex3f(br.X, br.Y, br.Z)
		rl.Color4ub(d.tint.R, d.tint.G, d.tint.B, vtxAlpha(tr.Y))
		rl.TexCoord2f(ou+1, ov+1)
		rl.Vertex3f(tr.X, tr.Y, tr.Z)
		rl.Color4ub(d.tint.R, d.tint.G, d.tint.B, vtxAlpha(tl.Y))
		rl.TexCoord2f(ou, ov+1)
		rl.Vertex3f(tl.X, tl.Y, tl.Z)
		rl.End()
	}

	rl.SetTexture(0)
	rl.EndShaderMode()
	rl.EnableDepthMask()
	p.batch = p.batch[:0]
}

func (p *particleRenderer) unload() {
	if !p.ok {
		return
	}
	rl.UnloadTexture(p.noise)
	rl.UnloadShader(p.shader)
	p.ok = false
}

// smokeEase: quick ease-in over the first 12% of life, smooth ease-out over
// the last 45%.
func smokeEase(ageFrac float32) float32 {
	in := ageFrac / 0.12
	if in > 1 {
		in = 1
	}
	out := (1 - ageFrac) / 0.45
	if out > 1 {
		out = 1
	}
	if out < 0 {
		out = 0
	}
	return in * out * (3 - 2*out) // smooth the tail, keep the attack sharp
}

func puffSeed(a, b uint32) uint32 {
	h := a*2654435761 + b*40503 + 0x9E3779B9
	h ^= h >> 13
	return h * 0x85EBCA6B
}

func hash01(seed uint32) float32 {
	seed ^= seed >> 16
	seed *= 0x7FEB352D
	seed ^= seed >> 15
	return float32(seed&0xFFFF) / 65535.0
}

func sinCos(a float32) (float32, float32) {
	s, c := math.Sincos(float64(a))
	return float32(s), float32(c)
}
