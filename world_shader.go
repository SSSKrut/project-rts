package main

import (
	"image"
	"math"

	rl "github.com/gen2brain/raylib-go/raylib"

	"rts-go/components"
	"rts-go/gen/textures"
	"rts-go/systems"
)

// The world's ground material: a directional sun plus a set of procedurally
// generated surfaces (turf, baked soil, gravel, sand) blended by height, slope
// and a patch mask. Chunk meshes carry no UVs, so everything is sampled from
// world XZ — no seams between chunks, nothing to bake, and the blend follows
// the terrain rather than the mesh.
const (
	// 100x100 texels per m² is the surface's working resolution; the texture
	// side then decides how many metres a tile spans before it repeats.
	surfaceTexSize    = 512
	surfaceTexelsPerM = 100
	surfaceTileMeters = float32(surfaceTexSize) / surfaceTexelsPerM

	// The coarse companion is stretched ~9x and carries broad undulation only,
	// so it needs no resolution of its own.
	macroTexSize   = 256
	macroScale     = 9.0
	macroWeight    = 0.7
	macroFadeStart = 220
	macroFadeEnd   = 460

	detailStrength   = 0.7
	detailAO         = 0.85
	detailTint       = 0.22
	detailFadeStartM = 30
	detailFadeEndM   = 110

	// How wide a soil / gravel patch runs before it turns back to turf, in
	// tile multiples.
	patchScale = 26.0

	surfaceSeed int64 = 0x4E4F524D414C2121
)

// Blend rule bands. Each pair is the (start, end) of a smoothstep, so every
// transition is a ramp rather than a line drawn on the ground.
var (
	// Heights follow reliefStops: procgen spans roughly -8..14 m, so sand
	// belongs to the wet lowland end and gravel to the exposed-rock end.
	sandBand  = [2]float32{-6.5, -3.0} // full sand below x, none above y
	rockBand  = [2]float32{5.5, 9.0}   // gravel takes over with altitude
	slopeBand = [2]float32{0.22, 0.55} // and with steepness
	patchBand = [2]float32{0.62, 0.16} // soil patch threshold + softness
)

// Sun low in the north-west: grazing light is what makes micro-relief
// readable — near the zenith a normal map flattens out. Ambient is a
// hemisphere, sky above and bounced ground below.
var (
	sunDir      = [3]float32{-0.52, 0.55, -0.46}
	sunColor    = [3]float32{0.82, 0.78, 0.68}
	skyColor    = [3]float32{0.55, 0.58, 0.63}
	bounceColor = [3]float32{0.34, 0.33, 0.29}
)

// surfaceSlot binds one generated surface to a material map slot, the sampler
// that reads it, and the albedo it imposes on the terrain's relief colour.
// Slot order IS weight order in the shader: adding a surface means a row here,
// a term in surfaceWeights() and a tap in blendSurfaces().
type surfaceSlot struct {
	sampler  string
	mapSlot  int32
	locIndex int32
	params   func(size int, seed int64) textures.GroundParams
	// Colour it pulls the ground towards; W is how strongly, 0 = leave the
	// height-derived relief colour alone.
	color [4]float32
}

var groundSurfaces = [4]surfaceSlot{
	{"texture0", rl.MapAlbedo, rl.ShaderLocMapAlbedo, textures.MeadowParams,
		[4]float32{0.42, 0.55, 0.28, 0.0}},
	{"texture1", rl.MapMetalness, rl.ShaderLocMapMetalness, textures.DrySoilParams,
		[4]float32{0.46, 0.36, 0.24, 0.72}},
	{"texture3", rl.MapRoughness, rl.ShaderLocMapRoughness, textures.GravelParams,
		[4]float32{0.44, 0.43, 0.40, 0.78}},
	{"texture4", rl.MapOcclusion, rl.ShaderLocMapOcclusion, textures.SandParams,
		[4]float32{0.74, 0.68, 0.50, 0.82}},
}

const worldVS = `#version 330
in vec3 vertexPosition;
in vec3 vertexNormal;
in vec4 vertexColor;

uniform mat4 mvp;
uniform mat4 matModel;
uniform mat4 matNormal;

out vec3 fragPosition;
out vec3 fragNormal;
out vec4 fragColor;

void main() {
    fragPosition = (matModel * vec4(vertexPosition, 1.0)).xyz;
    fragNormal = normalize((matNormal * vec4(vertexNormal, 0.0)).xyz);
    fragColor = vertexColor;
    gl_Position = mvp * vec4(vertexPosition, 1.0);
}
`

// Surface maps carry RG slope, B occlusion, A albedo variation.
const worldFS = `#version 330
in vec3 fragPosition;
in vec3 fragNormal;
in vec4 fragColor;

uniform sampler2D texture0;   // turf
uniform sampler2D texture1;   // baked soil
uniform sampler2D texture2;   // coarse companion
uniform sampler2D texture3;   // gravel
uniform sampler2D texture4;   // sand
uniform sampler2D texture5;   // cloud pair-slice noise

uniform vec4 uSurfaceColor[4];
uniform vec2 uSandBand;
uniform vec2 uRockBand;
uniform vec2 uSlopeBand;
uniform vec2 uPatchBand;
uniform float uPatchScale;

uniform vec2 uWorldOffset;
uniform float uTileMeters;
uniform float uGround;        // 1 on terrain, 0 on roads / props / buildings
uniform vec3 uSunDir;
uniform vec3 uSunColor;
uniform vec3 uSkyColor;
uniform vec3 uBounceColor;
uniform vec2 uFade;
uniform vec2 uMacroFade;
uniform vec2 uMacro;          // x = tile multiplier, y = weight
uniform vec2 uSurface;        // x = AO strength, y = albedo variation strength
uniform vec3 uCamPos;
uniform vec2 uCloudWind;      // accumulated wind drift, metres (matches clouds)
uniform vec2 uShadowOfs;      // sun-projected offset of the cloud layer
uniform float uShadowMidY;    // mid-layer height x shape frequency
uniform float uCloudCover;    // 0 disables cloud shadows
uniform float uShadowOn;      // 0 for batch objects: texture5 is unbound there

out vec4 finalColor;

// Mirrors the cloud pass's density field (same noise, wind and thresholds) so
// ground shadows track the actual puffs; sampled at mid-layer height, offset
// along the sun so shadows land where the light says they should.
float cnoise3(vec3 x) {
    vec3 p = floor(x);
    vec3 f = fract(x);
    f = f * f * (3.0 - 2.0 * f);
    vec2 uv = (p.xy + vec2(37.0, 239.0) * p.z) + f.xy;
    vec2 rg = textureLod(texture5, (uv + 0.5) / 256.0, 0.0).yx;
    return mix(rg.x, rg.y, f.z);
}

float cfbm(vec3 p) {
    float a = 0.5;
    float s = 0.0;
    for (int i = 0; i < 4; i++) {
        s += a * cnoise3(p);
        p *= 2.02;
        a *= 0.5;
    }
    return s;
}

float cloudShadow(vec2 wxz) {
    vec2 sw = wxz + uShadowOfs + uCloudWind;
    float cov = cfbm(vec3(sw.x * 0.00042, 7.31, sw.y * 0.00042));
    cov = smoothstep(1.0 - uCloudCover, 1.0 - uCloudCover + 0.35, cov);
    if (cov <= 0.001) return 1.0;
    float sh = cfbm(vec3(sw.x * 0.0011, uShadowMidY, sw.y * 0.0011));
    float d = clamp((sh * 0.95 - (1.0 - cov * 0.85)) * 4.0, 0.0, 1.0);
    return 1.0 - 0.62 * d;
}

// Weights are ordered exactly like groundSurfaces: turf, soil, gravel, sand.
vec4 surfaceWeights(float height, float steepness, float patch) {
    float sand = 1.0 - smoothstep(uSandBand.x, uSandBand.y, height);
    float rock = max(smoothstep(uRockBand.x, uRockBand.y, height),
                     smoothstep(uSlopeBand.x, uSlopeBand.y, steepness));
    float soil = smoothstep(uPatchBand.x, uPatchBand.x + uPatchBand.y, patch)
                 * (1.0 - rock) * (1.0 - sand);
    float turf = max(1.0 - sand - rock - soil, 0.0);
    vec4 w = vec4(turf, soil, rock, sand);
    return w / max(w.x + w.y + w.z + w.w, 0.0001);
}

vec4 blendSurfaces(vec2 uv, vec4 w) {
    vec4 acc = vec4(0.0);
    if (w.x > 0.004) acc += texture(texture0, uv) * w.x;
    if (w.y > 0.004) acc += texture(texture1, uv) * w.y;
    if (w.z > 0.004) acc += texture(texture3, uv) * w.z;
    if (w.w > 0.004) acc += texture(texture4, uv) * w.w;
    return acc;
}

void main() {
    vec3 n = normalize(fragNormal);
    vec3 albedo = fragColor.rgb;
    float ao = 1.0;

    if (uGround > 0.5) {
        vec2 world = fragPosition.xz + uWorldOffset;
        float dist = distance(fragPosition, uCamPos);

        // The patch mask reuses the coarse map at a far larger scale — surfaces
        // have to change over tens of metres, not per texel. Its 133 m period
        // reads as obvious tiling on the far rings where no detail texture
        // breaks it up, so the patches dissolve with distance.
        float patch = texture(texture2, world / (uTileMeters * uPatchScale)).w;
        patch *= 1.0 - smoothstep(400.0, 1500.0, dist);
        vec4 w = surfaceWeights(fragPosition.y, 1.0 - n.y, patch);

        // Surface colour applies at every distance: a sand flat has to read as
        // sand from across the map. Relief only survives while it is legible.
        for (int i = 0; i < 4; i++) {
            albedo = mix(albedo, uSurfaceColor[i].rgb, uSurfaceColor[i].w * w[i]);
        }

        // Two bands of the same surface set. The fine one dies with distance —
        // past its fade a texel is far under a pixel and the mip chain flattens
        // it anyway; the coarse one carries the form beyond that.
        float near = 1.0 - smoothstep(uFade.x, uFade.y, dist);
        float far = uMacro.y * (1.0 - smoothstep(uMacroFade.x, uMacroFade.y, dist));

        if (near + far > 0.001) {
            vec4 fine = blendSurfaces(world / uTileMeters, w);
            vec4 coarse = texture(texture2, world / (uTileMeters * uMacro.x));

            vec2 slope = (fine.xy * 2.0 - 1.0) * near + (coarse.xy * 2.0 - 1.0) * far;
            // No tangent attribute on chunk meshes: build the frame from world
            // X projected onto the surface. Exact on flat ground, good enough
            // on a slope where the terrain normal dominates anyway.
            vec3 tangent = normalize(vec3(1.0, 0.0, 0.0) - n * n.x);
            vec3 bitangent = cross(n, tangent);
            n = normalize(n + tangent * slope.x + bitangent * slope.y);

            ao = mix(1.0, fine.z, near * uSurface.x) * mix(1.0, coarse.z, far * uSurface.x);
            float variation = (fine.w * 2.0 - 1.0) * near + (coarse.w * 2.0 - 1.0) * far;
            albedo *= 1.0 + variation * uSurface.y;
        }
    }

    float sunShadow = 1.0;
    if (uShadowOn > 0.5 && uCloudCover > 0.001) {
        sunShadow = cloudShadow(fragPosition.xz + uWorldOffset);
    }
    vec3 ambient = mix(uBounceColor, uSkyColor, 0.5 + 0.5 * n.y);
    vec3 light = (ambient + uSunColor * max(dot(n, uSunDir), 0.0) * sunShadow) * ao;
    finalColor = vec4(albedo * light, fragColor.a);
}
`

// worldShader owns the ground material's shader and its surface maps.
type worldShader struct {
	shader   rl.Shader
	surfaces [len(groundSurfaces)]rl.Texture2D
	macro    rl.Texture2D
	cloudTex rl.Texture2D

	locOffset      int32
	locGround      int32
	locCam         int32
	locCloudWind   int32
	locShadowOfs   int32
	locShadowMidY  int32
	locCloudCover  int32
	locShadowOn    int32

	ok bool
}

func newWorldShader() *worldShader {
	ws := &worldShader{}
	ws.shader = rl.LoadShaderFromMemory(worldVS, worldFS)
	if ws.shader.ID == 0 {
		return ws
	}

	for i := range groundSurfaces {
		s := &groundSurfaces[i]
		p := s.params(surfaceTexSize, surfaceSeed+int64(i)*17)
		p.Strength *= detailStrength
		ws.surfaces[i] = uploadSurface(textures.Ground(p), surfaceTexSize)
		// raylib only auto-resolves texture0/1/2; the rest need their sampler
		// wired to a map slot by hand, after which DrawMesh binds them.
		ws.shader.UpdateLocation(s.locIndex, rl.GetShaderLocation(ws.shader, s.sampler))
	}
	ws.macro = uploadSurface(
		textures.Ground(textures.SoftGroundParams(macroTexSize, surfaceSeed+7)), macroTexSize)
	ws.shader.UpdateLocation(rl.ShaderLocMapNormal, rl.GetShaderLocation(ws.shader, "texture2"))
	ws.cloudTex = uploadCloudNoise()
	ws.shader.UpdateLocation(rl.ShaderLocMapEmission, rl.GetShaderLocation(ws.shader, "texture5"))

	ws.locOffset = rl.GetShaderLocation(ws.shader, "uWorldOffset")
	ws.locGround = rl.GetShaderLocation(ws.shader, "uGround")
	ws.locCam = rl.GetShaderLocation(ws.shader, "uCamPos")
	ws.locCloudWind = rl.GetShaderLocation(ws.shader, "uCloudWind")
	ws.locShadowOfs = rl.GetShaderLocation(ws.shader, "uShadowOfs")
	ws.locShadowMidY = rl.GetShaderLocation(ws.shader, "uShadowMidY")
	ws.locCloudCover = rl.GetShaderLocation(ws.shader, "uCloudCover")
	ws.locShadowOn = rl.GetShaderLocation(ws.shader, "uShadowOn")

	colors := make([]float32, 0, len(groundSurfaces)*4)
	for i := range groundSurfaces {
		c := groundSurfaces[i].color
		colors = append(colors, c[0], c[1], c[2], c[3])
	}
	rl.SetShaderValueV(ws.shader, rl.GetShaderLocation(ws.shader, "uSurfaceColor"),
		colors, rl.ShaderUniformVec4, int32(len(groundSurfaces)))

	sun := normalize3(sunDir)
	set := func(name string, v []float32, kind rl.ShaderUniformDataType) {
		rl.SetShaderValue(ws.shader, rl.GetShaderLocation(ws.shader, name), v, kind)
	}
	set("uTileMeters", []float32{surfaceTileMeters}, rl.ShaderUniformFloat)
	set("uSunDir", sun[:], rl.ShaderUniformVec3)
	set("uSunColor", sunColor[:], rl.ShaderUniformVec3)
	set("uSkyColor", skyColor[:], rl.ShaderUniformVec3)
	set("uBounceColor", bounceColor[:], rl.ShaderUniformVec3)
	set("uSurface", []float32{detailAO, detailTint}, rl.ShaderUniformVec2)
	set("uFade", []float32{detailFadeStartM, detailFadeEndM}, rl.ShaderUniformVec2)
	set("uMacro", []float32{macroScale, macroWeight}, rl.ShaderUniformVec2)
	set("uMacroFade", []float32{macroFadeStart, macroFadeEnd}, rl.ShaderUniformVec2)
	set("uSandBand", sandBand[:], rl.ShaderUniformVec2)
	set("uRockBand", rockBand[:], rl.ShaderUniformVec2)
	set("uSlopeBand", slopeBand[:], rl.ShaderUniformVec2)
	set("uPatchBand", patchBand[:], rl.ShaderUniformVec2)
	set("uPatchScale", []float32{patchScale}, rl.ShaderUniformFloat)

	ws.ok = true
	return ws
}

// apply points a material at this shader and fills its map slots with the
// surface set.
func (ws *worldShader) apply(mat *rl.Material) {
	if !ws.ok {
		return
	}
	mat.Shader = ws.shader
	// Not SetMaterialTexture: it hands cgo a Go pointer holding another Go
	// pointer (Material.Maps) and panics under the pointer checks.
	for i := range groundSurfaces {
		mat.GetMap(groundSurfaces[i].mapSlot).Texture = ws.surfaces[i]
	}
	mat.GetMap(rl.MapNormal).Texture = ws.macro
	mat.GetMap(rl.MapEmission).Texture = ws.cloudTex
}

// release takes the borrowed handles back out of a material before it is
// unloaded. UnloadMaterial frees the shader and every non-default texture it
// finds, but those belong to worldShader — leaving them in place made the
// shutdown path RL_FREE(shader.locs) twice and SIGSEGV (ISSUES #27). Zeroed
// ids are no-ops for glDeleteProgram / glDeleteTextures, so UnloadMaterial is
// left with just the map array raylib allocated itself. Unconditional (not
// gated on ok) so it cannot be defeated by unload() clearing the flag first.
func (ws *worldShader) release(mat *rl.Material) {
	if mat == nil {
		return
	}
	mat.Shader = rl.Shader{}
	if mat.Maps == nil {
		return
	}
	for i := range groundSurfaces {
		mat.GetMap(groundSurfaces[i].mapSlot).Texture.ID = 0
	}
	mat.GetMap(rl.MapNormal).Texture.ID = 0
	mat.GetMap(rl.MapEmission).Texture.ID = 0
}

// beginFrame re-anchors the UV origin (the render origin shifts by whole
// chunks as the camera travels), updates the camera position the fade needs
// and feeds the cloud-shadow uniforms from the Atmosphere resource.
func (ws *worldShader) beginFrame(atm *components.Atmosphere, drift rl.Vector2) {
	if !ws.ok {
		return
	}
	rl.SetShaderValue(ws.shader, ws.locOffset, []float32{
		float32(systems.CurrentOriginChunk.X) * components.ChunkSize,
		float32(systems.CurrentOriginChunk.Z) * components.ChunkSize,
	}, rl.ShaderUniformVec2)
	cam := systems.CurrentCamera.Position
	rl.SetShaderValue(ws.shader, ws.locCam,
		[]float32{cam.X, cam.Y, cam.Z}, rl.ShaderUniformVec3)
	cover := float32(0)
	if debugOverlay.Clouds && atm != nil {
		cover = atm.Coverage
	}
	if atm != nil {
		mid := (atm.CloudBase + atm.CloudTop) * 0.5
		sun := normalize3(sunDir)
		rl.SetShaderValue(ws.shader, ws.locShadowOfs,
			[]float32{-sun[0] / sun[1] * mid, -sun[2] / sun[1] * mid}, rl.ShaderUniformVec2)
		rl.SetShaderValue(ws.shader, ws.locShadowMidY,
			[]float32{mid * 0.0022}, rl.ShaderUniformFloat)
	}
	rl.SetShaderValue(ws.shader, ws.locCloudWind,
		[]float32{drift.X, drift.Y}, rl.ShaderUniformVec2)
	rl.SetShaderValue(ws.shader, ws.locCloudCover, []float32{cover}, rl.ShaderUniformFloat)
	rl.SetShaderValue(ws.shader, ws.locShadowOn, []float32{1}, rl.ShaderUniformFloat)
}

// setGround switches the surface blend between draw groups: terrain gets it,
// road and water ribbons stay smooth and keep their own colours.
func (ws *worldShader) setGround(on bool) {
	if !ws.ok {
		return
	}
	v := float32(0)
	if on && debugOverlay.TerrainDetail {
		v = 1
	}
	rl.SetShaderValue(ws.shader, ws.locGround, []float32{v}, rl.ShaderUniformFloat)
}

// beginObjects puts immediate-mode geometry (props, buildings) under the same
// sun so the scene lights as one world. The surface blend stays off: those
// meshes carry no ground UVs.
func (ws *worldShader) beginObjects() {
	if !ws.ok {
		return
	}
	ws.setGround(false)
	// Batch geometry has no material maps, so texture5 is unbound there —
	// uShadowOn keeps the shadow term out of those fragments.
	rl.SetShaderValue(ws.shader, ws.locShadowOn, []float32{0}, rl.ShaderUniformFloat)
	rl.BeginShaderMode(ws.shader)
}

func (ws *worldShader) endObjects() {
	if !ws.ok {
		return
	}
	rl.EndShaderMode()
	rl.SetShaderValue(ws.shader, ws.locShadowOn, []float32{1}, rl.ShaderUniformFloat)
}

func (ws *worldShader) unload() {
	if !ws.ok {
		return
	}
	for i := range ws.surfaces {
		rl.UnloadTexture(ws.surfaces[i])
	}
	rl.UnloadTexture(ws.macro)
	rl.UnloadTexture(ws.cloudTex)
	rl.UnloadShader(ws.shader)
	ws.ok = false
}

// uploadSurface hands an RGBA buffer to the GPU with mips and repeat wrap.
func uploadSurface(pix []uint8, size int) rl.Texture2D {
	img := &image.RGBA{
		Pix:    pix,
		Stride: size * 4,
		Rect:   image.Rect(0, 0, size, size),
	}
	rlImg := rl.NewImageFromImage(img)
	tex := rl.LoadTextureFromImage(rlImg)
	rl.UnloadImage(rlImg)
	rl.GenTextureMipmaps(&tex)
	rl.SetTextureFilter(tex, rl.FilterTrilinear)
	rl.SetTextureWrap(tex, rl.WrapRepeat)
	return tex
}

func normalize3(v [3]float32) [3]float32 {
	l := v[0]*v[0] + v[1]*v[1] + v[2]*v[2]
	if l <= 0 {
		return [3]float32{0, 1, 0}
	}
	inv := 1 / float32(math.Sqrt(float64(l)))
	return [3]float32{v[0] * inv, v[1] * inv, v[2] * inv}
}
