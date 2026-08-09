package main

import (
	"encoding/binary"
	"log"
	"math"

	rl "github.com/gen2brain/raylib-go/raylib"

	"rts-go/components"
)

// Event cues (Phase 19.7 M3). A compressed run has to be audible: the banner
// is a visual signal for a player who is already looking at the right corner
// of the screen, and at 32x that assumption is exactly what breaks. Tones are
// synthesised at boot — no assets to ship, and the pitch pattern per kind is
// the whole vocabulary. Voice radio ("Contact!") is Phase 24; it needs a cast.

// audioCueGap is per kind: a firefight pushes suppression events in bursts and
// the point is to be told once, not to be buzzed at.
const audioCueGap float64 = 2.0

const (
	cueSampleRate  = 22050
	cueVolume      = 0.55
	cueFadeSamples = 220 // ~10 ms attack/release, enough to kill the click
)

type audioCues struct {
	ready  bool
	Muted  bool
	sounds [components.EventKindCount]rl.Sound
	loaded [components.EventKindCount]bool
	lastAt [components.EventKindCount]float64
}

// cueTone is one segment of a cue; Freq 0 is a rest.
type cueTone struct {
	Freq float32
	Ms   int
}

// cuePatterns: rising pair reads as "look", a single low note as "lost", a
// triple blip as "pinned", a falling pair as "refused".
var cuePatterns = map[components.EventKind][]cueTone{
	components.EventEnemyContact:     {{880, 90}, {0, 40}, {1175, 110}},
	components.EventKIA:              {{300, 260}},
	components.EventSuppressionStart: {{620, 60}, {0, 45}, {620, 60}, {0, 45}, {620, 60}},
	components.EventOrderFailed:      {{700, 100}, {0, 30}, {430, 150}},
	components.EventOrderCompleted:   {{990, 80}},
}

// initAudioCues is a no-op headless: a gate run has no business opening an
// audio device, and the frame half that would play a cue never executes.
func (g *Game) initAudioCues() {
	if g.headless {
		return
	}
	rl.InitAudioDevice()
	if !rl.IsAudioDeviceReady() {
		log.Printf("audio: device not ready, event cues disabled")
		return
	}
	g.UI.Cues.ready = true
	for kind, pattern := range cuePatterns {
		wav := synthCueWAV(pattern)
		wave := rl.LoadWaveFromMemory(".wav", wav, int32(len(wav)))
		snd := rl.LoadSoundFromWave(wave)
		rl.UnloadWave(wave)
		rl.SetSoundVolume(snd, cueVolume)
		g.UI.Cues.sounds[kind] = snd
		g.UI.Cues.loaded[kind] = true
	}
}

func (g *Game) shutdownAudioCues() {
	if !g.UI.Cues.ready {
		return
	}
	for k := range g.UI.Cues.loaded {
		if g.UI.Cues.loaded[k] {
			rl.UnloadSound(g.UI.Cues.sounds[k])
		}
	}
	rl.CloseAudioDevice()
	g.UI.Cues.ready = false
}

// playEventCue is rate-limited per kind on the wall clock, independent of the
// attention cooldown: the clock reacts once to the loudest thing, the ear
// wants to hear each distinct kind.
func (g *Game) playEventCue(kind components.EventKind) {
	c := &g.UI.Cues
	if !c.ready || c.Muted || int(kind) >= len(c.loaded) || !c.loaded[kind] {
		return
	}
	now := rl.GetTime()
	if now-c.lastAt[kind] < audioCueGap {
		return
	}
	c.lastAt[kind] = now
	rl.PlaySound(c.sounds[kind])
}

// synthCueWAV renders a pattern into a 16-bit mono RIFF buffer. Going through
// a WAV image rather than a raw rl.Wave keeps the sample data on the Go side
// until raylib copies it, so there is no lifetime question to get wrong.
func synthCueWAV(pattern []cueTone) []byte {
	total := 0
	for _, t := range pattern {
		total += t.Ms * cueSampleRate / 1000
	}
	samples := make([]int16, 0, total)
	for _, t := range pattern {
		n := t.Ms * cueSampleRate / 1000
		for i := 0; i < n; i++ {
			if t.Freq <= 0 {
				samples = append(samples, 0)
				continue
			}
			phase := 2 * math.Pi * float64(t.Freq) * float64(i) / cueSampleRate
			env := segmentEnvelope(i, n)
			samples = append(samples, int16(math.Sin(phase)*env*32000))
		}
	}

	const headerLen = 44
	dataLen := len(samples) * 2
	buf := make([]byte, headerLen+dataLen)
	copy(buf[0:], "RIFF")
	binary.LittleEndian.PutUint32(buf[4:], uint32(36+dataLen))
	copy(buf[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(buf[16:], 16)              // fmt chunk size
	binary.LittleEndian.PutUint16(buf[20:], 1)               // PCM
	binary.LittleEndian.PutUint16(buf[22:], 1)               // mono
	binary.LittleEndian.PutUint32(buf[24:], cueSampleRate)   // sample rate
	binary.LittleEndian.PutUint32(buf[28:], cueSampleRate*2) // byte rate
	binary.LittleEndian.PutUint16(buf[32:], 2)               // block align
	binary.LittleEndian.PutUint16(buf[34:], 16)              // bits per sample
	copy(buf[36:], "data")
	binary.LittleEndian.PutUint32(buf[40:], uint32(dataLen))
	for i, s := range samples {
		binary.LittleEndian.PutUint16(buf[headerLen+i*2:], uint16(s))
	}
	return buf
}

// segmentEnvelope fades each segment in and out; a bare sine switched on at
// full amplitude clicks louder than the tone itself.
func segmentEnvelope(i, n int) float64 {
	fade := cueFadeSamples
	if n < 2*fade {
		fade = n / 2
	}
	if fade <= 0 {
		return 1
	}
	switch {
	case i < fade:
		return float64(i) / float64(fade)
	case i >= n-fade:
		return float64(n-i) / float64(fade)
	}
	return 1
}
