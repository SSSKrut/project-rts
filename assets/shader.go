package assets

import rl "github.com/gen2brain/raylib-go/raylib"

// The baked models carry no textures at all: the flattened material colour
// lives in COLOR_0, one material for the whole vehicle. So this shader is
// deliberately thin — vertex colour, one sun, a hemisphere ambient and the
// same distance fog the rest of the scene uses.
//
// Vertex colours are linear (glTF says so, and the bake records which space it
// wrote). Everything else in this renderer is authored in raylib's sRGB-ish
// byte palette, so the albedo is encoded on the way in rather than the whole
// frame being converted on the way out.
const modelVS = `#version 330
in vec3 vertexPosition;
in vec3 vertexNormal;
in vec4 vertexColor;
uniform mat4 mvp;
uniform mat4 matModel;
uniform mat4 matNormal;
out vec3 fragNormal;
out vec3 fragWorld;
out vec4 fragColor;
void main() {
    fragColor  = vertexColor;
    fragNormal = normalize(vec3(matNormal * vec4(vertexNormal, 1.0)));
    fragWorld  = vec3(matModel * vec4(vertexPosition, 1.0));
    gl_Position = mvp * vec4(vertexPosition, 1.0);
}
`

const modelFS = `#version 330
in vec3 fragNormal;
in vec3 fragWorld;
in vec4 fragColor;
uniform vec4 colDiffuse;
uniform vec3 uSunDir;
uniform vec3 uSunColor;
uniform vec3 uSkyColor;
uniform vec3 uBounceColor;
uniform vec3 uCamPos;
uniform vec3 uFogColor;
uniform vec2 uFog;
uniform float uEncode;
out vec4 finalColor;
void main() {
    vec3 base = fragColor.rgb;
    if (uEncode > 0.5) base = pow(max(base, vec3(0.0)), vec3(1.0 / 2.2));
    base *= colDiffuse.rgb;

    // Wrapped diffuse rather than a hard N.L: a vehicle read from above at a
    // shallow angle has most of its area facing away from the sun, and a
    // clamped term leaves those panels near black. Half-lambert keeps the
    // silhouette legible without washing out the lit faces.
    vec3 n = normalize(fragNormal);
    float wrap = dot(n, uSunDir) * 0.5 + 0.5;
    float sky = 0.5 + 0.5 * n.y;
    vec3 lit = base * (uSunColor * wrap * 0.85
                     + mix(uBounceColor, uSkyColor, sky) * 0.45);

    float d = length(fragWorld - uCamPos);
    float f = clamp((d - uFog.x) / max(uFog.y - uFog.x, 1.0), 0.0, 1.0);
    finalColor = vec4(mix(lit, uFogColor, f), fragColor.a * colDiffuse.a);
}
`

// Shader is the model program plus its uniform locations. It borrows nothing
// and owns its own program, so unloading is a plain UnloadShader — but any
// material pointed at it must be released first (ISSUES #27: UnloadMaterial
// frees a shader it merely borrowed).
type Shader struct {
	shader rl.Shader
	ok     bool

	locSunDir, locSunColor, locSkyColor, locBounce int32
	locCamPos, locFogColor, locFog, locEncode      int32
}

// Light is the per-frame lighting the caller feeds in; in game these come from
// the daylight palette so vehicles sit under the same sun as the terrain.
type Light struct {
	SunDir   rl.Vector3
	SunColor rl.Vector3
	SkyColor rl.Vector3
	Bounce   rl.Vector3
	FogColor rl.Vector3
	FogStart float32
	FogEnd   float32
}

// DefaultLight is a plain overcast noon, for sandboxes and first light.
func DefaultLight() Light {
	return Light{
		SunDir:   rl.Vector3{X: 0.42, Y: 0.80, Z: 0.43},
		SunColor: rl.Vector3{X: 0.95, Y: 0.92, Z: 0.85},
		SkyColor: rl.Vector3{X: 0.42, Y: 0.48, Z: 0.58},
		Bounce:   rl.Vector3{X: 0.28, Y: 0.27, Z: 0.22},
		FogColor: rl.Vector3{X: 0.62, Y: 0.66, Z: 0.72},
		FogStart: 260,
		FogEnd:   900,
	}
}

func newShader() Shader {
	s := Shader{}
	s.shader = rl.LoadShaderFromMemory(modelVS, modelFS)
	s.ok = s.shader.ID != 0
	if !s.ok {
		return s
	}
	loc := func(n string) int32 { return rl.GetShaderLocation(s.shader, n) }
	s.locSunDir, s.locSunColor = loc("uSunDir"), loc("uSunColor")
	s.locSkyColor, s.locBounce = loc("uSkyColor"), loc("uBounceColor")
	s.locCamPos, s.locFogColor = loc("uCamPos"), loc("uFogColor")
	s.locFog, s.locEncode = loc("uFog"), loc("uEncode")
	return s
}

// SetLight uploads the frame's lighting. Call once per frame, not per model.
func (r *Registry) SetLight(l Light, camPos rl.Vector3) {
	s := &r.shader
	if !s.ok {
		return
	}
	v3 := func(loc int32, v rl.Vector3) {
		rl.SetShaderValue(s.shader, loc, []float32{v.X, v.Y, v.Z}, rl.ShaderUniformVec3)
	}
	d := rl.Vector3Normalize(l.SunDir)
	v3(s.locSunDir, d)
	v3(s.locSunColor, l.SunColor)
	v3(s.locSkyColor, l.SkyColor)
	v3(s.locBounce, l.Bounce)
	v3(s.locCamPos, camPos)
	v3(s.locFogColor, l.FogColor)
	rl.SetShaderValue(s.shader, s.locFog,
		[]float32{l.FogStart, l.FogEnd}, rl.ShaderUniformVec2)
}

// setEncode tells the shader whether this asset's COLOR_0 is linear and needs
// encoding for a renderer that is not colour managed. Cached: the value only
// changes when a differently-baked asset is drawn.
func (r *Registry) setEncode(on bool) {
	if !r.shader.ok || (r.encodeKnown && r.encodeSet == on) {
		return
	}
	r.encodeKnown = true
	v := float32(0)
	if on {
		v = 1
	}
	rl.SetShaderValue(r.shader.shader, r.shader.locEncode,
		[]float32{v}, rl.ShaderUniformFloat)
	r.encodeSet = on
}
