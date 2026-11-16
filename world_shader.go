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
// generated surfaces (turf, soil, scree, sand and cliff rock) blended by
// height, slope, biome and river masks. Surface UVs are derived from world XZ
// (triplanar on cliffs), so the blend follows terrain rather than chunk meshes.
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
	macroWeight    = 0.32
	macroFadeStart = 220
	macroFadeEnd   = 460

	detailStrength   = 0.38
	detailAO         = 0.7
	detailTint       = 0.16
	detailFadeStartM = 30
	detailFadeEndM   = 110

	// Soil and grass vary independently, so a dry meadow does not necessarily
	// become bare earth. Scales are in surface-tile multiples.
	patchScale     = 29.0
	meadowScale    = 17.0
	uvWarpScale    = 37.0
	uvWarpMeters   = 0.65
	cliffTileScale = 1.65

	surfaceSeed int64 = 0x4E4F524D414C2121
)

// Blend rule bands. Each pair is the (start, end) of a smoothstep, so every
// transition is a ramp rather than a line drawn on the ground.
var (
	surfaceTileScales = [4]float32{1.35, 1.05, 0.82, 1.60}

	patchBand = [2]float32{0.56, 0.20} // soil patch threshold + softness

	meadowLushColor = [3]float32{0.30, 0.41, 0.20}
	meadowDryColor  = [3]float32{0.49, 0.49, 0.28}
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
	// Colour targets and blend strength. Turf uses the meadow colour ramp.
	color    [4]float32
	altColor [3]float32
}

var groundSurfaces = [5]surfaceSlot{
	{"texture0", rl.MapAlbedo, rl.ShaderLocMapAlbedo, textures.MeadowParams,
		[4]float32{0.40, 0.46, 0.24, 0.88}, [3]float32{0.34, 0.43, 0.22}},
	{"texture1", rl.MapMetalness, rl.ShaderLocMapMetalness, textures.DrySoilParams,
		[4]float32{0.42, 0.33, 0.23, 0.82}, [3]float32{0.50, 0.40, 0.28}},
	{"texture3", rl.MapRoughness, rl.ShaderLocMapRoughness, textures.GravelParams,
		[4]float32{0.44, 0.43, 0.40, 0.88}, [3]float32{0.32, 0.31, 0.29}},
	{"texture4", rl.MapOcclusion, rl.ShaderLocMapOcclusion, textures.SandParams,
		[4]float32{0.68, 0.62, 0.45, 0.85}, [3]float32{0.55, 0.48, 0.34}},
	{"texture6", rl.MapHeight, rl.ShaderLocMapHeight, textures.CliffParams,
		[4]float32{0.36, 0.35, 0.33, 0.92}, [3]float32{0.50, 0.48, 0.43}},
}

const worldVS = `#version 330
in vec3 vertexPosition;
in vec3 vertexNormal;
in vec4 vertexColor;
in vec2 vertexTexCoord;
in vec2 vertexTexCoord2;

uniform mat4 mvp;
uniform mat4 matModel;
uniform mat4 matNormal;

out vec3 fragPosition;
out vec3 fragNormal;
out vec4 fragColor;
out vec4 fragBiome;

void main() {
    fragPosition = (matModel * vec4(vertexPosition, 1.0)).xyz;
    fragNormal = normalize((matNormal * vec4(vertexNormal, 0.0)).xyz);
    fragColor = vertexColor;
    fragBiome = vec4(vertexTexCoord, vertexTexCoord2);
    gl_Position = mvp * vec4(vertexPosition, 1.0);
}
`

// Surface maps carry RG slope, B occlusion, A albedo variation.
const worldFS = `#version 330
in vec3 fragPosition;
in vec3 fragNormal;
in vec4 fragColor;
in vec4 fragBiome;

uniform sampler2D texture0;   // turf
uniform sampler2D texture1;   // baked soil
uniform sampler2D texture2;   // coarse companion
uniform sampler2D texture3;   // gravel
uniform sampler2D texture4;   // sand
uniform sampler2D texture5;   // cloud pair-slice noise
uniform sampler2D texture6;   // stratified cliff rock

uniform vec4 uSurfaceColor[5];
uniform vec3 uSurfaceAltColor[5];
uniform vec2 uSandBand;
uniform vec2 uRockBand;
uniform vec2 uSlopeBand;
uniform vec2 uPatchBand;
uniform float uPatchScale;
uniform float uMeadowScale;
uniform vec3 uMeadowLush;
uniform vec3 uMeadowDry;

uniform vec2 uWorldOffset;
uniform float uTileMeters;
uniform float uGround;        // 1 on terrain, 0 on roads / props / buildings
uniform float uTerrainDrop;   // far-ring depth offset; restore height for material rules
uniform vec3 uSunDir;
uniform vec3 uSunColor;
uniform vec3 uSkyColor;
uniform vec3 uBounceColor;
uniform vec2 uFade;
uniform vec2 uMacroFade;
uniform vec2 uMacro;          // x = tile multiplier, y = weight
uniform vec4 uSurfaceTiles;   // per-material tile multipliers
uniform float uCliffTile;
uniform vec2 uWarp;           // x = mask tile multiplier, y = displacement in metres
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

struct SurfaceWeights {
    float turf;
    float soil;
    float gravel;
    float sand;
    float cliff;
};

// Weights follow groundSurfaces: altitude creates loose scree, while steep
// faces become stratified rock rather than stretched gravel.
SurfaceWeights surfaceWeights(float height, float steepness, float patch, vec4 biome) {
    SurfaceWeights w;
    w.cliff = smoothstep(uSlopeBand.x, uSlopeBand.y, steepness);
    w.sand = max(1.0 - smoothstep(uSandBand.x, uSandBand.y, height),
                 biome.w * 0.72 * (1.0 - w.cliff));
    float altitudeGravel = smoothstep(uRockBand.x, uRockBand.y, height);
    float biomeGravel = smoothstep(0.50, 0.86, biome.z) * 0.85;
    w.gravel = max(altitudeGravel, biomeGravel) * (1.0 - w.cliff) * (1.0 - w.sand);
    float soilPatch = smoothstep(uPatchBand.x, uPatchBand.x + uPatchBand.y, patch);
    float forestFloor = smoothstep(0.50, 0.86, biome.x) * 0.58;
    w.soil = max(soilPatch, forestFloor)
             * (1.0 - w.gravel) * (1.0 - w.sand) * (1.0 - w.cliff);
    w.turf = max(1.0 - w.sand - w.gravel - w.soil - w.cliff, 0.0);
    float invSum = 1.0 / max(w.turf + w.soil + w.gravel + w.sand + w.cliff, 0.0001);
    w.turf *= invSum;
    w.soil *= invSum;
    w.gravel *= invSum;
    w.sand *= invSum;
    w.cliff *= invSum;
    return w;
}

vec2 surfaceUV(vec2 p, float tileScale, float angle) {
    float c = cos(angle);
    float s = sin(angle);
    p = mat2(c, s, -s, c) * p;
    return p / (uTileMeters * tileScale);
}

vec4 sampleCliff(vec3 p, vec3 n, out vec3 cliffNormal) {
    vec3 blend = pow(abs(n), vec3(4.0));
    blend /= max(blend.x + blend.y + blend.z, 0.0001);
    float tile = uTileMeters * uCliffTile;
    vec4 tx = texture(texture6, p.zy / tile);
    vec4 ty = texture(texture6, p.xz / tile);
    vec4 tz = texture(texture6, p.xy / tile);
    vec2 sx = tx.xy * 2.0 - 1.0;
    vec2 sy = ty.xy * 2.0 - 1.0;
    vec2 sz = tz.xy * 2.0 - 1.0;
    vec3 nx = normalize(vec3(n.x < 0.0 ? -1.0 : 1.0, sx.y, sx.x));
    vec3 ny = normalize(vec3(sy.x, 1.0, sy.y));
    vec3 nz = normalize(vec3(sz.x, sz.y, n.z < 0.0 ? -1.0 : 1.0));
    cliffNormal = normalize(nx * blend.x + ny * blend.y + nz * blend.z);
    return tx * blend.x + ty * blend.y + tz * blend.z;
}

vec4 blendSurfaces(vec2 world, float height, vec3 n, SurfaceWeights w, out vec3 cliffNormal) {
    cliffNormal = n;
    vec4 acc = vec4(0.0);
    if (w.turf > 0.004) acc += texture(texture0, surfaceUV(world, uSurfaceTiles.x, 0.0)) * w.turf;
    if (w.soil > 0.004) acc += texture(texture1, surfaceUV(world, uSurfaceTiles.y, 0.31)) * w.soil;
    if (w.gravel > 0.004) acc += texture(texture3, surfaceUV(world, uSurfaceTiles.z, -0.23)) * w.gravel;
    if (w.sand > 0.004) acc += texture(texture4, surfaceUV(world, uSurfaceTiles.w, 0.58)) * w.sand;
    if (w.cliff > 0.004) acc += sampleCliff(vec3(world.x, height, world.y), n, cliffNormal) * w.cliff;
    return acc;
}

void main() {
    vec3 n = normalize(fragNormal);
    vec3 albedo = fragColor.rgb;
    float ao = 1.0;

    if (uGround > 0.5) {
        vec2 world = fragPosition.xz + uWorldOffset;
        float dist = distance(fragPosition, uCamPos);

        // Coarse, offset masks form independent grass and bare-earth regions.
        // World-space sampling keeps both continuous across chunk boundaries.
        float patch = texture(texture2, world / (uTileMeters * uPatchScale)).w;
        patch *= 1.0 - smoothstep(400.0, 1500.0, dist);
        float meadow = texture(texture2,
            world / (uTileMeters * uMeadowScale) + vec2(0.37, 0.61)).w;
        float surfaceHeight = fragPosition.y + uTerrainDrop + (meadow - 0.5) * 1.6;
        float steepness = clamp(1.0 - n.y + (patch - 0.5) * 0.08, 0.0, 1.0);
        SurfaceWeights w = surfaceWeights(surfaceHeight, steepness, patch, fragBiome);

        float tone = smoothstep(0.24, 0.76, meadow);
        float dryness = clamp(smoothstep(0.28, 0.74, meadow)
                        + fragBiome.y * 0.22 - fragBiome.x * 0.18, 0.0, 1.0);
        vec3 turf = mix(uMeadowLush, uMeadowDry, dryness);
        vec3 surfaceColor = turf * (uSurfaceColor[0].w * w.turf);
        float colorWeight = uSurfaceColor[0].w * w.turf;
        for (int i = 1; i < 5; i++) {
            float cw = uSurfaceColor[i].w * (i == 1 ? w.soil : i == 2 ? w.gravel : i == 3 ? w.sand : w.cliff);
            surfaceColor += mix(uSurfaceColor[i].rgb, uSurfaceAltColor[i], tone) * cw;
            colorWeight += cw;
        }
        albedo = mix(albedo, surfaceColor / max(colorWeight, 0.0001), clamp(colorWeight, 0.0, 1.0));
        albedo *= 1.0 - fragBiome.w * 0.24;

        // Two bands of the same surface set. The fine one dies with distance —
        // past its fade a texel is far under a pixel and the mip chain flattens
        // it anyway; the coarse one carries the form beyond that.
        float near = 1.0 - smoothstep(uFade.x, uFade.y, dist);
        float far = uMacro.y * (1.0 - smoothstep(uMacroFade.x, uMacroFade.y, dist));

        if (near + far > 0.001) {
            vec4 fine = vec4(0.5, 0.5, 1.0, 0.5);
            vec3 cliffNormal = n;
            if (near > 0.001) {
                vec2 warp = texture(texture2,
                    world / (uTileMeters * uWarp.x) + vec2(0.19, 0.47)).xy * 2.0 - 1.0;
                vec2 detailWorld = world + warp * uWarp.y;
                fine = blendSurfaces(detailWorld, surfaceHeight, n, w, cliffNormal);
            }
            vec4 coarse = texture(texture2, world / (uTileMeters * uMacro.x));

            vec2 slope = (fine.xy * 2.0 - 1.0) * near + (coarse.xy * 2.0 - 1.0) * far;
            // No tangent attribute on chunk meshes: build the frame from world
            // X projected onto the surface. Exact on flat ground, good enough
            // on a slope where the terrain normal dominates anyway.
            vec3 tangent = vec3(1.0, 0.0, 0.0) - n * n.x;
            if (dot(tangent, tangent) < 0.0001) tangent = cross(n, vec3(0.0, 0.0, 1.0));
            tangent = normalize(tangent);
            vec3 bitangent = cross(n, tangent);
            n = normalize(n + tangent * slope.x + bitangent * slope.y);
            n = normalize(mix(n, cliffNormal, w.cliff * near));

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
	locTerrainDrop int32
	locCam         int32
	locCloudWind   int32
	locShadowOfs   int32
	locShadowMidY  int32
	locCloudCover  int32
	locShadowOn    int32
	locSunDir      int32
	locSunColor    int32
	locSkyColor    int32
	locBounce      int32

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
	ws.locTerrainDrop = rl.GetShaderLocation(ws.shader, "uTerrainDrop")
	ws.locCam = rl.GetShaderLocation(ws.shader, "uCamPos")
	ws.locCloudWind = rl.GetShaderLocation(ws.shader, "uCloudWind")
	ws.locShadowOfs = rl.GetShaderLocation(ws.shader, "uShadowOfs")
	ws.locShadowMidY = rl.GetShaderLocation(ws.shader, "uShadowMidY")
	ws.locCloudCover = rl.GetShaderLocation(ws.shader, "uCloudCover")
	ws.locShadowOn = rl.GetShaderLocation(ws.shader, "uShadowOn")
	ws.locSunDir = rl.GetShaderLocation(ws.shader, "uSunDir")
	ws.locSunColor = rl.GetShaderLocation(ws.shader, "uSunColor")
	ws.locSkyColor = rl.GetShaderLocation(ws.shader, "uSkyColor")
	ws.locBounce = rl.GetShaderLocation(ws.shader, "uBounceColor")

	colors := make([]float32, 0, len(groundSurfaces)*4)
	for i := range groundSurfaces {
		c := groundSurfaces[i].color
		colors = append(colors, c[0], c[1], c[2], c[3])
	}
	rl.SetShaderValueV(ws.shader, rl.GetShaderLocation(ws.shader, "uSurfaceColor"),
		colors, rl.ShaderUniformVec4, int32(len(groundSurfaces)))
	altColors := make([]float32, 0, len(groundSurfaces)*3)
	for i := range groundSurfaces {
		c := groundSurfaces[i].altColor
		altColors = append(altColors, c[0], c[1], c[2])
	}
	rl.SetShaderValueV(ws.shader, rl.GetShaderLocation(ws.shader, "uSurfaceAltColor"),
		altColors, rl.ShaderUniformVec3, int32(len(groundSurfaces)))

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
	set("uSurfaceTiles", surfaceTileScales[:], rl.ShaderUniformVec4)
	set("uCliffTile", []float32{cliffTileScale}, rl.ShaderUniformFloat)
	set("uWarp", []float32{uvWarpScale, uvWarpMeters}, rl.ShaderUniformVec2)
	set("uMacroFade", []float32{macroFadeStart, macroFadeEnd}, rl.ShaderUniformVec2)
	surfaceParams := systems.CurrentSurfaceParams()
	set("uSandBand", surfaceParams.SandBand[:], rl.ShaderUniformVec2)
	set("uRockBand", surfaceParams.RockBand[:], rl.ShaderUniformVec2)
	set("uSlopeBand", surfaceParams.SlopeBand[:], rl.ShaderUniformVec2)
	set("uPatchBand", patchBand[:], rl.ShaderUniformVec2)
	set("uPatchScale", []float32{patchScale}, rl.ShaderUniformFloat)
	set("uMeadowScale", []float32{meadowScale}, rl.ShaderUniformFloat)
	set("uMeadowLush", meadowLushColor[:], rl.ShaderUniformVec3)
	set("uMeadowDry", meadowDryColor[:], rl.ShaderUniformVec3)

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
// chunks as the camera travels), updates the camera position the fade needs,
// feeds the cloud-shadow uniforms from the Atmosphere resource and the light
// set from the daylight palette.
func (ws *worldShader) beginFrame(atm *components.Atmosphere, drift rl.Vector2, pal *skyPalette) {
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
	ws.setTerrainDrop(0)
	setVec3(ws.shader, ws.locSunDir, pal.lightDir)
	setVec3(ws.shader, ws.locSunColor, pal.lightCol)
	setVec3(ws.shader, ws.locSkyColor, pal.ambSky)
	setVec3(ws.shader, ws.locBounce, pal.ambBounce)
	cover := float32(0)
	if debugOverlay.Clouds && atm != nil {
		cover = atm.Coverage
	}
	if atm != nil {
		mid := (atm.CloudBase + atm.CloudTop) * 0.5
		// shadowDir is elevation-clamped: a horizon sun would project the
		// layer kilometres away; the shadow term only scales the direct
		// light, which is near zero then anyway.
		sun := pal.shadowDir
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
func (ws *worldShader) setTerrainDrop(drop float32) {
	if !ws.ok {
		return
	}
	rl.SetShaderValue(ws.shader, ws.locTerrainDrop, []float32{drop}, rl.ShaderUniformFloat)
}

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
	rl.SetTextureFilter(tex, rl.FilterAnisotropic8x)
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
