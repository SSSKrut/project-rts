package main

// The whole backdrop is one fullscreen fragment shader: deep space, a dust
// band with nebulae, three star layers, and an analytically raytraced planet
// in the middle. Nothing is loaded from disk, so the menu boots as fast as a
// window can open.

const spaceVS = `#version 330
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

const spaceFS = `#version 330
out vec4 finalColor;

uniform vec2  uRes;      // window size in px
uniform float uTime;     // seconds
uniform vec2  uPar;      // parallax offset of the starfield, uv units
uniform vec3  uSunDir;   // direction TO the sun (view space, +z toward viewer)
uniform vec2  uCtr;      // planet centre in uv units
uniform float uRad;      // planet radius in uv units
uniform float uFade;     // 0 at boot, 1 once faded in

float hash12(vec2 p) {
    vec3 p3 = fract(vec3(p.xyx) * 0.1031);
    p3 += dot(p3, p3.yzx + 33.33);
    return fract((p3.x + p3.y) * p3.z);
}

vec2 hash22(vec2 p) {
    vec3 p3 = fract(vec3(p.xyx) * vec3(0.1031, 0.1030, 0.0973));
    p3 += dot(p3, p3.yzx + 33.33);
    return fract((p3.xx + p3.yz) * p3.zy);
}

float hash13(vec3 p3) {
    p3 = fract(p3 * 0.1031);
    p3 += dot(p3, p3.zyx + 31.32);
    return fract((p3.x + p3.y) * p3.z);
}

float noise3(vec3 x) {
    vec3 i = floor(x);
    vec3 f = fract(x);
    f = f * f * (3.0 - 2.0 * f);
    float a = mix(mix(hash13(i + vec3(0, 0, 0)), hash13(i + vec3(1, 0, 0)), f.x),
                  mix(hash13(i + vec3(0, 1, 0)), hash13(i + vec3(1, 1, 0)), f.x), f.y);
    float b = mix(mix(hash13(i + vec3(0, 0, 1)), hash13(i + vec3(1, 0, 1)), f.x),
                  mix(hash13(i + vec3(0, 1, 1)), hash13(i + vec3(1, 1, 1)), f.x), f.y);
    return mix(a, b, f.z);
}

// Octaves are rotated as well as scaled: without it every octave shares the
// lattice of the one below and the low frequencies show up as square blotches.
const mat3 OCT = mat3(0.00, 0.80, 0.60, -0.80, 0.36, -0.48, -0.60, -0.48, 0.64);

float fbm(vec3 p, int oct) {
    float a = 0.5, s = 0.0;
    for (int i = 0; i < 7; i++) {
        if (i >= oct) break;
        s += a * noise3(p);
        p = OCT * p * 2.02;
        a *= 0.5;
    }
    return s;
}

mat3 rotY(float a) {
    float c = cos(a), s = sin(a);
    return mat3(c, 0.0, -s, 0.0, 1.0, 0.0, s, 0.0, c);
}

mat3 rotZ(float a) {
    float c = cos(a), s = sin(a);
    return mat3(c, s, 0.0, -s, c, 0.0, 0.0, 0.0, 1.0);
}

// The galactic plane: a soft diagonal band that carries the dust haze and
// concentrates the faint stars, so the sky reads as a direction rather than
// as uniform noise.
float milkyBand(vec2 uv) {
    const float a = -0.40;
    float y = uv.x * sin(a) + uv.y * cos(a) + 0.06;
    float w = 0.22 + 0.16 * fbm(vec3(uv * 1.1, 3.0), 3);
    float b = exp(-(y * y) / (w * w));
    return b * (0.55 + 0.60 * fbm(vec3(uv * 2.2 + 12.0, 7.0), 4));
}

// Barely-there clouds of gas. Two masks: one says where a nebula exists at
// all, the other shapes it — without the first, faint colour smears over the
// whole frame and the black stops being black.
vec3 nebula(vec2 uv) {
    vec3 p = vec3(uv * 2.3, 11.0);
    vec3 w = vec3(fbm(p * 1.7 + 3.0, 3), fbm(p * 1.7 + 17.0, 3), fbm(p * 1.7 + 41.0, 2));
    float n = fbm(p + w * 1.3, 6);
    float mask = smoothstep(0.44, 0.86, fbm(p * 0.30 + 31.0, 3));
    float dens = smoothstep(0.40, 0.92, n) * mask;

    float tone = fbm(p * 0.31 + 47.0, 2);
    vec3 c = mix(vec3(0.05, 0.15, 0.52), vec3(0.42, 0.10, 0.26), smoothstep(0.36, 0.72, tone));
    c = mix(c, vec3(0.04, 0.30, 0.30), smoothstep(0.62, 0.30, tone) * 0.55);
    return c * dens;
}

// One decade of star magnitudes. dens keeps most cells empty; pow(m, 6) makes
// bright stars rare, which is what stops a starfield looking like scattered
// salt. The core radius is measured in uv (i.e. in pixels), never in cells —
// sizing it per layer is what turns distant stars into cotton balls.
vec3 starLayer(vec2 uv, float scale, float dens, float bright, float twinkle, float spikes) {
    vec2 p = uv * scale;
    vec2 c = floor(p);
    vec2 f = fract(p) - 0.5;
    float px = 1.0 / uRes.y;
    vec3 acc = vec3(0.0);
    for (int j = -1; j <= 1; j++) {
        for (int i = -1; i <= 1; i++) {
            vec2 o = vec2(float(i), float(j));
            vec2 id = c + o;
            float k = hash12(id);
            if (k > dens) continue;
            vec2 jitter = (hash22(id + 5.7) - 0.5) * 0.85;
            vec2 cell = f - o - jitter;
            vec2 duv = cell / scale;
            // The neighbourhood is 3x3, so anything reaching past 1.5 cells
            // gets clipped on a square edge — a halo must fade out first.
            float win = smoothstep(1.45, 0.55, max(abs(cell.x), abs(cell.y)));
            if (win <= 0.0) continue;
            float m = pow(hash12(id + 7.1), 6.0);
            float t = hash12(id + 3.3);
            vec3 col = mix(vec3(0.78, 0.85, 1.00), vec3(1.00, 0.88, 0.72), t);

            float rr = px * (0.85 + m * 2.2);
            float d2 = dot(duv, duv);
            float core = exp(-d2 / (rr * rr));
            float halo = exp(-sqrt(d2) / (rr * 4.5)) * m * 0.20 * win;
            float tw = mix(1.0, 0.78 + 0.22 * sin(uTime * (0.5 + t * 1.9) + k * 44.0), twinkle);
            acc += col * (core + halo) * (0.55 + m * 3.2) * bright * tw;

            if (spikes > 0.0 && m > 0.12) {
                vec2 a = abs(duv) / px;
                float sp = exp(-a.x * 0.11) * exp(-a.y * 3.5)
                         + exp(-a.y * 0.11) * exp(-a.x * 3.5);
                acc += col * sp * m * spikes * bright * tw * win;
            }
        }
    }
    return acc;
}

// Continents. Value noise clusters hard around 0.5, so the raw field gives a
// mushy coastline; the contrast stretch is what makes land read as land.
float landHeight(vec3 sp) {
    vec3 q = sp * 1.45;
    vec3 w = vec3(fbm(q * 2.4 + 3.0, 3), fbm(q * 2.4 + 9.0, 3), fbm(q * 2.4 + 21.0, 3));
    float h = fbm(q + w * 1.05, 6);
    return clamp((h - 0.5) * 2.1 + 0.5, 0.0, 1.0);
}

void main() {
    vec2 uv = (gl_FragCoord.xy - 0.5 * uRes) / uRes.y;
    vec2 buv = uv + uPar;

    // ---- deep space -------------------------------------------------------
    float band = milkyBand(buv);
    vec3 col = vec3(0.008, 0.011, 0.022) + vec3(0.026, 0.030, 0.052) * band * 0.85;
    col += nebula(buv) * (0.20 + 0.95 * band) * 1.35;

    col += starLayer(buv,              14.0, 0.26, 0.62, 1.0, 0.060) * (0.55 + 0.60 * band);
    col += starLayer(buv * 1.7 + 4.0,  34.0, 0.19, 0.26, 0.6, 0.010) * (0.28 + 0.95 * band);
    col += starLayer(buv * 2.9 + 9.0,  72.0, 0.15, 0.11, 0.2, 0.0)   * (0.16 + 1.05 * band);

    // The sun itself sits off-frame; only its trace reaches us.
    vec2 sunUV = normalize(uSunDir.xy) * 1.30;
    float sd = length(uv - sunUV);
    col += vec3(0.45, 0.55, 0.85) * 0.055 / (1.0 + sd * sd * 6.0);

    // ---- planet -----------------------------------------------------------
    vec2 d = uv - uCtr;
    float r = length(d);
    float px = 1.0 / uRes.y;
    vec2 sunXY = normalize(uSunDir.xy);

    if (r > uRad) {
        float e = (r - uRad) / (uRad * 0.13);
        float halo = exp(-e * 2.9);
        // Scattering happens on the lit limb; the night side must not wear a
        // ring, or the planet reads as a decal with a glow filter on it.
        float side = pow(clamp(dot(d / max(r, 1e-5), sunXY) * 0.5 + 0.5, 0.0, 1.0), 3.0);
        col += vec3(0.19, 0.41, 0.88) * halo * (0.045 + 0.955 * side) * 1.15;
    }

    if (r < uRad + 2.0 * px) {
        float z = sqrt(max(uRad * uRad - r * r, 1e-6));
        vec3 n = vec3(d, z) / uRad;
        mat3 tilt = rotZ(-0.36);
        vec3 sp = rotY(uTime * 0.010) * tilt * n;

        float h = landHeight(sp);
        float lat = abs(sp.y);
        float sea = smoothstep(0.508, 0.523, h);

        vec3 ocean = mix(vec3(0.008, 0.028, 0.070), vec3(0.026, 0.098, 0.145),
                         smoothstep(0.36, 0.508, h));
        float arid = smoothstep(0.60, 0.12, lat) * smoothstep(0.36, 0.72, fbm(sp * 3.4 + 60.0, 4));
        vec3 land = mix(vec3(0.062, 0.092, 0.048), vec3(0.245, 0.190, 0.112), arid);
        land = mix(land, vec3(0.045, 0.070, 0.042), smoothstep(0.55, 0.30, lat) * 0.55);
        land = mix(land, vec3(0.155, 0.145, 0.130), smoothstep(0.63, 0.78, h));
        float ice = smoothstep(0.86, 0.955, lat + 0.045 * fbm(sp * 7.0, 4));
        vec3 alb = mix(ocean, land, sea);
        alb = mix(alb, vec3(0.82, 0.87, 0.94), ice);

        float ndl = dot(n, uSunDir);
        float day = smoothstep(-0.11, 0.24, ndl);
        float term = smoothstep(0.34, 0.0, abs(ndl));
        vec3 sunCol = mix(vec3(1.00, 0.96, 0.90), vec3(1.00, 0.60, 0.34), term * 0.85);
        vec3 surf = alb * (day * sunCol * 1.20 + vec3(0.018, 0.028, 0.052));

        vec3 hv = normalize(uSunDir + vec3(0.0, 0.0, 1.0));
        float spec = pow(max(dot(n, hv), 0.0), 420.0) * (1.0 - sea) * (1.0 - ice) * day;
        surf += sunCol * spec * 1.1;

        // Weather runs in latitude belts — an equatorial convergence band and
        // a mid-latitude storm belt — otherwise the cover is one even smear.
        vec3 cp = rotY(uTime * 0.016 + 0.9) * tilt * n;
        vec3 cw = vec3(fbm(cp * 5.0 + 13.0, 3), fbm(cp * 5.0 + 29.0, 3), 0.0);
        float cf = fbm(cp * 4.2 + cw * 0.55 + vec3(0.0, uTime * 0.004, 0.0), 5);
        float belt = 0.30
                   + 0.75 * exp(-pow(lat / 0.16, 2.0))
                   + 0.85 * exp(-pow((lat - 0.68) / 0.20, 2.0));
        float cl = smoothstep(0.53, 0.70, cf * (0.55 + 0.60 * belt));
        cl *= 0.55 + 0.45 * smoothstep(0.99, 0.55, lat);
        vec3 cloudLit = (day * sunCol * 1.22 + vec3(0.026, 0.036, 0.064)) * vec3(0.94, 0.96, 0.99);
        surf = mix(surf, cloudLit, cl * 0.90);

        // City lights: only on land, only at night, thinned out by cloud and
        // by latitude. One hash cell lands at roughly a pixel at this radius.
        float night = smoothstep(0.10, -0.22, ndl);
        if (night > 0.001) {
            float k = hash13(floor(sp * 340.0));
            // Population clusters: coastal, temperate, and clumped by a low
            // frequency field, so lights read as cities rather than as dust.
            float coast = smoothstep(0.508, 0.545, h) * smoothstep(0.70, 0.56, h);
            float clump = smoothstep(0.42, 0.72, fbm(sp * 4.0 + 77.0, 4));
            float pop = sea * smoothstep(0.90, 0.35, lat) * (0.35 * coast + 0.85 * clump);
            float lit = step(1.0 - 0.085 * pop, k);
            float tw = 0.70 + 0.30 * sin(uTime * 1.7 + k * 60.0);
            surf += vec3(1.00, 0.72, 0.36) * lit * night * (1.0 - cl * 0.85) * 0.85 * tw;
        }

        float fres = pow(1.0 - clamp(z / uRad, 0.0, 1.0), 3.2);
        float sunFace = clamp(ndl * 1.7 + 0.40, 0.0, 1.0);
        surf += mix(vec3(0.17, 0.42, 0.96), vec3(1.00, 0.48, 0.24), term * 0.75) * fres * sunFace * 0.60;

        col = mix(col, surf, smoothstep(uRad + px, uRad - px, r));
    }

    // ---- grade ------------------------------------------------------------
    col *= 1.0 - 0.38 * smoothstep(0.50, 1.30, length(uv * vec2(0.80, 1.0)));
    col *= uFade;
    col = clamp((col * (2.51 * col + 0.03)) / (col * (2.43 * col + 0.59) + 0.14), 0.0, 1.0);
    col += (hash12(gl_FragCoord.xy + fract(uTime)) - 0.5) / 255.0;

    finalColor = vec4(col, 1.0);
}
`
