package main

import (
	"flag"
	"image"
	"math"
	"math/rand"

	rl "github.com/gen2brain/raylib-go/raylib"

	"rts-go/components"
	"rts-go/systems"
	"rts-go/ui"
)

// The cloud pass is the scene composite: instead of blitting the scene RT
// verbatim, the same quad runs a raymarch shader that reads scene color +
// scene depth, replaces the RayWhite background with a procedural sky and
// layers volumetric cumulus on top. Terrain occludes clouds through the depth
// texture; everything happens in render space so the sim never sees it.
const (
	cloudNoiseSize = 256
	cloudNoiseSeed = 0x436C6F75647321

	// Shared clip planes: set once at boot (SetClipPlanes) and fed to the
	// cloud pass's depth linearization — they must never diverge.
	renderNearPlane = 0.3
	renderFarPlane  = 16000.0
)

var cloudCoverFlag = flag.Float64("cloud-cover", 0.62, "dev: cloud coverage 0..1")

var cloudCoverage float32 = 0.62

const cloudVS = `#version 330
in vec3 vertexPosition;
in vec2 vertexTexCoord;
in vec4 vertexColor;
uniform mat4 mvp;
out vec2 fragTexCoord;
out vec4 fragColor;
void main() {
    fragTexCoord = vertexTexCoord;
    fragColor = vertexColor;
    gl_Position = mvp * vec4(vertexPosition, 1.0);
}
`

const cloudFS = `#version 330
in vec2 fragTexCoord;
in vec4 fragColor;

uniform sampler2D texture0;   // scene color
uniform sampler2D uDepth;     // scene depth (same orientation as color)
uniform sampler2D uNoise;     // 256x256 pair-slice value noise

uniform vec3 uCamPos;
uniform vec3 uCamFwd;
uniform vec3 uCamRight;
uniform vec3 uCamUp;
uniform vec2 uTanFov;         // tan(fov/2): x horizontal, y vertical
uniform vec4 uPanel;          // content rect x, yTop, w, h (window px, top-left)
uniform float uScreenH;
uniform vec2 uWorldOffset;    // origin-chunk world XZ
uniform float uTime;
uniform vec3 uSunDir;
uniform vec3 uSunColor;
uniform vec2 uNearFar;
uniform float uCoverage;

out vec4 finalColor;

const float CB = 350.0;
const float CT = 800.0;
const float LAYER = CT - CB;
const int STEPS = 54;
const int LSTEPS = 5;
const float SIGMA = 0.028;

// One bilinear tap = two adjacent Z slices: R(u,v) == G(u+37, v+239).
float noise3(vec3 x) {
    vec3 p = floor(x);
    vec3 f = fract(x);
    f = f * f * (3.0 - 2.0 * f);
    vec2 uv = (p.xy + vec2(37.0, 239.0) * p.z) + f.xy;
    vec2 rg = textureLod(uNoise, (uv + 0.5) / 256.0, 0.0).yx;
    return mix(rg.x, rg.y, f.z);
}

float fbm(vec3 p) {
    float a = 0.5;
    float s = 0.0;
    for (int i = 0; i < 4; i++) {
        s += a * noise3(p);
        p *= 2.02;
        a *= 0.5;
    }
    return s;
}

float cloudMap(vec3 p, float detail) {
    float h = (p.y - CB) / LAYER;
    if (h < 0.0 || h > 1.0) return 0.0;
    vec3 q = vec3(p.x + uWorldOffset.x, p.y, p.z + uWorldOffset.y);
    q.xz += uTime * vec2(9.0, 4.0);
    float cov = fbm(vec3(q.x * 0.00042, 7.31, q.z * 0.00042));
    cov = smoothstep(1.0 - uCoverage, 1.0 - uCoverage + 0.35, cov);
    if (cov <= 0.001) return 0.0;
    float prof = smoothstep(0.0, 0.14, h) * (1.0 - smoothstep(0.42, 1.0, h));
    float shape = fbm(q * vec3(0.0011, 0.0022, 0.0011));
    float d = clamp((shape * prof - (1.0 - cov * 0.85)) * 4.0, 0.0, 1.0);
    if (detail > 0.5 && d > 0.0 && d < 0.9) {
        float e = fbm(q * 0.0052);
        d = clamp(d - e * 0.30 * (1.0 - d), 0.0, 1.0);
    }
    return d;
}

float lightTrans(vec3 p) {
    float acc = 0.0;
    float st = 28.0;
    for (int i = 0; i < LSTEPS; i++) {
        p += uSunDir * st;
        acc += cloudMap(p, 0.0) * st;
        st *= 1.4;
    }
    return exp(-acc * SIGMA * 0.75);
}

float hg(float c, float g) {
    float g2 = g * g;
    return (1.0 - g2) / (4.0 * 3.14159 * pow(1.0 + g2 - 2.0 * g * c, 1.5));
}

vec3 skyColor(vec3 rd) {
    float t = clamp(rd.y * 1.6 + 0.15, 0.0, 1.0);
    vec3 c = mix(vec3(0.80, 0.84, 0.89), vec3(0.32, 0.49, 0.72), pow(t, 0.65));
    if (rd.y < 0.0) {
        // Below the horizon with no geometry = ground past the far plane;
        // tint the haze toward a ground tone so the void reads as distant land.
        c = mix(c, vec3(0.58, 0.62, 0.57), clamp(-rd.y * 2.5, 0.0, 0.65));
    }
    float s = max(dot(rd, uSunDir), 0.0);
    c += uSunColor * (0.20 * pow(s, 6.0) + 1.1 * pow(s, 400.0));
    return c;
}

void main() {
    vec2 local = vec2(gl_FragCoord.x - uPanel.x, (uScreenH - gl_FragCoord.y) - uPanel.y);
    vec2 ndc = vec2(2.0 * local.x / uPanel.z - 1.0, 1.0 - 2.0 * local.y / uPanel.w);
    vec3 rd = normalize(uCamFwd + uCamRight * ndc.x * uTanFov.x + uCamUp * ndc.y * uTanFov.y);

    // With near = 0.01 the depth curve spends almost all its range inside the
    // first hundred metres, so "is this sky" must be asked in eye-space, never
    // as a raw-depth threshold.
    float d0 = texture(uDepth, fragTexCoord).r;
    float zn = uNearFar.x;
    float zf = uNearFar.y;
    float zndc = d0 * 2.0 - 1.0;
    float zeye = 2.0 * zn * zf / (zf + zn - zndc * (zf - zn));
    bool skyPx = zeye > zf * 0.995;
    float sceneDist = skyPx ? 1e8 : zeye / max(dot(rd, uCamFwd), 1e-3);

    vec3 base = texture(texture0, fragTexCoord).rgb;
    if (skyPx) {
        base = skyColor(rd);
    } else {
        // Distant ground dissolves into exactly what the sky would be along
        // this ray, so the far-terrain rings melt into the horizon seamlessly.
        base = mix(base, skyColor(rd), 1.0 - exp(-sceneDist * 0.00022));
    }

    // Slab entry/exit for the cloud layer.
    float t0 = 0.0;
    float t1 = -1.0;
    vec3 ro = uCamPos;
    if (abs(rd.y) < 1e-4) {
        if (ro.y >= CB && ro.y <= CT) { t0 = 0.0; t1 = 20000.0; }
    } else {
        float ta = (CB - ro.y) / rd.y;
        float tb = (CT - ro.y) / rd.y;
        t0 = max(min(ta, tb), 0.0);
        t1 = max(ta, tb);
    }
    t1 = min(min(t1, sceneDist), 20000.0);
    t1 = min(t1, t0 + 4200.0);

    if (t1 > t0) {
        float mu = dot(rd, uSunDir);
        float ph = mix(hg(mu, -0.15), hg(mu, 0.58), 0.7) * 12.5;
        float dt = (t1 - t0) / float(STEPS);
        float dith = fract(52.9829189 * fract(dot(gl_FragCoord.xy, vec2(0.06711056, 0.00583715))));
        float t = t0 + dith * dt;
        vec3 acc = vec3(0.0);
        float T = 1.0;
        for (int i = 0; i < STEPS; i++) {
            if (t > t1) break;
            vec3 p = ro + rd * t;
            float den = cloudMap(p, 1.0);
            if (den > 0.004) {
                float h = clamp((p.y - CB) / LAYER, 0.0, 1.0);
                float lt = lightTrans(p);
                float pw = 1.0 - 0.55 * exp(-den * 6.0);
                vec3 amb = mix(vec3(0.50, 0.54, 0.62), vec3(0.98, 1.00, 1.04), h);
                vec3 S = uSunColor * lt * (ph * pw + 0.8 * sqrt(lt)) + amb * 0.62;
                float aT = exp(-den * SIGMA * dt);
                acc += T * (1.0 - aT) * S;
                T *= aT;
                if (T < 0.01) break;
            }
            t += dt;
        }
        float a = 1.0 - T;
        if (a > 0.002) {
            vec3 cc = acc / a;
            float peak = max(cc.r, max(cc.g, cc.b));
            cc /= 1.0 + 0.25 * max(peak - 1.0, 0.0);
            float haze = 1.0 - exp(-max(t0 - 900.0, 0.0) * 0.00028);
            cc = mix(cc, skyColor(rd), haze);
            base = mix(base, cc, a);
        }
    }

    finalColor = vec4(base, 1.0);
}
`

type cloudRenderer struct {
	shader rl.Shader
	noise  rl.Texture2D

	locDepth   int32
	locNoise   int32
	locCamPos  int32
	locFwd     int32
	locRight   int32
	locUp      int32
	locTanFov  int32
	locPanel   int32
	locScreenH int32
	locWorld   int32
	locTime    int32
	locCover   int32

	ok bool
}

func newCloudRenderer() *cloudRenderer {
	cloudCoverage = float32(*cloudCoverFlag)
	c := &cloudRenderer{}
	c.shader = rl.LoadShaderFromMemory(cloudVS, cloudFS)
	if c.shader.ID == 0 {
		return c
	}
	c.noise = uploadCloudNoise()

	loc := func(name string) int32 { return rl.GetShaderLocation(c.shader, name) }
	c.locDepth = loc("uDepth")
	c.locNoise = loc("uNoise")
	c.locCamPos = loc("uCamPos")
	c.locFwd = loc("uCamFwd")
	c.locRight = loc("uCamRight")
	c.locUp = loc("uCamUp")
	c.locTanFov = loc("uTanFov")
	c.locPanel = loc("uPanel")
	c.locScreenH = loc("uScreenH")
	c.locWorld = loc("uWorldOffset")
	c.locTime = loc("uTime")
	c.locCover = loc("uCoverage")

	sun := normalize3(sunDir)
	rl.SetShaderValue(c.shader, loc("uSunDir"), sun[:], rl.ShaderUniformVec3)
	rl.SetShaderValue(c.shader, loc("uSunColor"), sunColor[:], rl.ShaderUniformVec3)
	rl.SetShaderValue(c.shader, loc("uNearFar"),
		[]float32{renderNearPlane, renderFarPlane}, rl.ShaderUniformVec2)

	c.ok = true
	return c
}

// composite replaces Scene3DRT.Composite: same quad, cloud shader on top.
func (c *cloudRenderer) composite(rt *ui.Scene3DRT, panel ui.Panel) {
	content := ui.ContentRect(panel)
	cam := systems.CurrentCamera

	fwd := vecNorm(rl.Vector3{
		X: cam.Target.X - cam.Position.X,
		Y: cam.Target.Y - cam.Position.Y,
		Z: cam.Target.Z - cam.Position.Z,
	})
	right := vecNorm(vecCross(fwd, cam.Up))
	up := vecCross(right, fwd)

	tanY := float32(math.Tan(float64(cam.Fovy) * math.Pi / 360.0))
	tanX := tanY * float32(rt.Width) / float32(rt.Height)

	set := func(l int32, v []float32, kind rl.ShaderUniformDataType) {
		rl.SetShaderValue(c.shader, l, v, kind)
	}
	set(c.locCamPos, []float32{cam.Position.X, cam.Position.Y, cam.Position.Z}, rl.ShaderUniformVec3)
	set(c.locFwd, []float32{fwd.X, fwd.Y, fwd.Z}, rl.ShaderUniformVec3)
	set(c.locRight, []float32{right.X, right.Y, right.Z}, rl.ShaderUniformVec3)
	set(c.locUp, []float32{up.X, up.Y, up.Z}, rl.ShaderUniformVec3)
	set(c.locTanFov, []float32{tanX, tanY}, rl.ShaderUniformVec2)
	set(c.locPanel, []float32{content.X, content.Y, content.Width, content.Height}, rl.ShaderUniformVec4)
	set(c.locScreenH, []float32{float32(rl.GetScreenHeight())}, rl.ShaderUniformFloat)
	set(c.locWorld, []float32{
		float32(systems.CurrentOriginChunk.X) * components.ChunkSize,
		float32(systems.CurrentOriginChunk.Z) * components.ChunkSize,
	}, rl.ShaderUniformVec2)
	set(c.locTime, []float32{float32(rl.GetTime())}, rl.ShaderUniformFloat)
	set(c.locCover, []float32{cloudCoverage}, rl.ShaderUniformFloat)

	rl.BeginShaderMode(c.shader)
	rl.SetShaderValueTexture(c.shader, c.locDepth, rt.RT.Depth)
	rl.SetShaderValueTexture(c.shader, c.locNoise, c.noise)
	src := rl.Rectangle{X: 0, Y: 0, Width: float32(rt.Width), Height: -float32(rt.Height)}
	rl.DrawTexturePro(rt.RT.Texture, src, content, rl.Vector2{}, 0, rl.White)
	rl.EndShaderMode()
}

func (c *cloudRenderer) unload() {
	if !c.ok {
		return
	}
	rl.UnloadTexture(c.noise)
	rl.UnloadShader(c.shader)
	c.ok = false
}

// uploadCloudNoise builds the pair-slice texture: G is white noise, R is G
// shifted by (37, 239) so one bilinear tap yields slice z and z+1.
func uploadCloudNoise() rl.Texture2D {
	const n = cloudNoiseSize
	rnd := rand.New(rand.NewSource(cloudNoiseSeed))
	g := make([]byte, n*n)
	for i := range g {
		g[i] = byte(rnd.Intn(256))
	}
	pix := make([]byte, n*n*4)
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			i := (y*n + x) * 4
			pix[i] = g[((y+239)&(n-1))*n+((x+37)&(n-1))]
			pix[i+1] = g[y*n+x]
			pix[i+2] = 0
			pix[i+3] = 255
		}
	}
	img := &image.RGBA{Pix: pix, Stride: n * 4, Rect: image.Rect(0, 0, n, n)}
	rlImg := rl.NewImageFromImage(img)
	tex := rl.LoadTextureFromImage(rlImg)
	rl.UnloadImage(rlImg)
	rl.SetTextureFilter(tex, rl.FilterBilinear)
	rl.SetTextureWrap(tex, rl.WrapRepeat)
	return tex
}

func vecCross(a, b rl.Vector3) rl.Vector3 {
	return rl.Vector3{
		X: a.Y*b.Z - a.Z*b.Y,
		Y: a.Z*b.X - a.X*b.Z,
		Z: a.X*b.Y - a.Y*b.X,
	}
}

func vecNorm(v rl.Vector3) rl.Vector3 {
	l := float32(math.Sqrt(float64(v.X*v.X + v.Y*v.Y + v.Z*v.Z)))
	if l < 1e-6 {
		return rl.Vector3{Y: 1}
	}
	return rl.Vector3{X: v.X / l, Y: v.Y / l, Z: v.Z / l}
}
