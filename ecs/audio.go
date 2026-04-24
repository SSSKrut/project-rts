package ecs

import (
"math"
"sort"

rl "github.com/gen2brain/raylib-go/raylib"
)

// AudioSource represents a continuous or spatial sound attached to an entity.
type AudioSource struct {
	SoundID     string
	IsPlaying   bool
	IsLooping   bool
	MaxDistance float32
	Volume      float32 // 0.0 to 1.0
}

// AudioVoice tracks a hardware playback channel for a specific sound.
type AudioVoice struct {
	HardwareSound rl.Sound
	Owner         EntityID // 0 if free
	IsPlaying     bool
	Vol           float32
}

// AudioManager pre-allocates raylib Sound objects to allow independent 
// volume and panning for multiple instances of the same loaded Wave.
type AudioManager struct {
	MaxVoicesPerSound int
	voices            map[string][]AudioVoice
}

// NewAudioManager initializes the audio manager with a global voice limit per sound.
func NewAudioManager(maxVoices int) *AudioManager {
	return &AudioManager{
		MaxVoicesPerSound: maxVoices,
		voices:            make(map[string][]AudioVoice),
	}
}

// RegisterWave converts a single memory wave into N independent hardware sounds.
func (am *AudioManager) RegisterWave(id string, wave rl.Wave) {
	pool := make([]AudioVoice, am.MaxVoicesPerSound)
	for i := 0; i < am.MaxVoicesPerSound; i++ {
		snd := rl.LoadSoundFromWave(wave)
		pool[i] = AudioVoice{HardwareSound: snd, Owner: 0}
	}
	am.voices[id] = pool
}

// Unload releases all hardware audio resources.
func (am *AudioManager) Unload() {
	for _, pool := range am.voices {
		for i := range pool {
			rl.UnloadSound(pool[i].HardwareSound)
		}
	}
}

// SpatialAudioSystem limits playing sounds per spatial chunk and maps them to hardware voices.
type SpatialAudioSystem struct {
	Manager     *AudioManager
	MaxPerChunk int
}

func (SpatialAudioSystem) Name() string { return "spatial_audio" }
func (SpatialAudioSystem) Phase() Phase { return PhasePostPhysics }

func (SpatialAudioSystem) LODPolicy() LODPolicy {
	return LODPolicy{
		ActiveEvery:   0, // Smooth panning/volume changes in near-field
		RelevantEvery: LODDisabled, // We could enable Relevant, but often we only want audio near the camera.
		DormantEvery:  LODDisabled,
	}
}

func (SpatialAudioSystem) Reads() []ComponentType {
	return []ComponentType{TypeOf[Position3D](), TypeOf[AudioSource](), TypeOf[LODAnchor]()}
}
func (SpatialAudioSystem) Writes() []ComponentType {
	return []ComponentType{TypeOf[AudioSource]()}
}

// audioCandidate groups entity info for spatial filtering
type audioCandidate struct {
	id     EntityID
	pos    Position3D
	dist   float32
	source AudioSource
}

func (sys SpatialAudioSystem) Update(ctx UpdateContext) {
	var anchorID EntityID
	var anchorPos Position3D
	found := false
	for _, id := range ctx.Current.Query(TypeOf[LODAnchor](), TypeOf[Position3D]()) {
		anchorPos, _ = Get[Position3D](ctx.Current, id)
		anchorID = id
		found = true
		break
	}
	if !found {
		return
	}

	// 1. Collect all playing audio candidates grouped by SoundID and ChunkID
	chunks := make(map[ChunkID]map[string][]audioCandidate)

	for _, id := range QueryLOD(ctx.Current, ctx.LOD, TypeOf[Position3D](), TypeOf[AudioSource]()) {
		if id == anchorID {
			continue // usually we don't handle listener's own ambient audio this way, but we could.
		}

		source, _ := Get[AudioSource](ctx.Current, id)
		if !source.IsPlaying {
			continue
		}

		pos, _ := Get[Position3D](ctx.Current, id)
		dx := pos.X - anchorPos.X
		dy := pos.Y - anchorPos.Y
		dz := pos.Z - anchorPos.Z
		dist := float32(math.Sqrt(float64(dx*dx + dy*dy + dz*dz)))

		if dist > source.MaxDistance {
			continue // Completely inaudible
		}

		chunk := ctx.Current.Grid.PosToChunk(pos.X, pos.Y, pos.Z)
		if chunks[chunk] == nil {
			chunks[chunk] = make(map[string][]audioCandidate)
		}
		chunks[chunk][source.SoundID] = append(chunks[chunk][source.SoundID], audioCandidate{
id: id, pos: pos, dist: dist, source: source,
})
	}

	// 2. Select permitted candidates (Enforce Per-Chunk Limit)
	var allowed []audioCandidate
	for _, soundMap := range chunks {
		for _, candidates := range soundMap {
			sort.Slice(candidates, func(i, j int) bool {
return candidates[i].dist < candidates[j].dist
			})
			limit := sys.MaxPerChunk
			if len(candidates) < limit {
				limit = len(candidates)
			}
			for i := 0; i < limit; i++ {
				allowed = append(allowed, candidates[i])
			}
		}
	}

	// 3. Allocate hardware voices globally by distance (if we have more allowed than voices)
	sort.Slice(allowed, func(i, j int) bool {
return allowed[i].dist < allowed[j].dist
	})

	// To quickly check who gets a voice this frame
	awardedVoices := make(map[EntityID]audioCandidate)

	// Keep track of how many voices we've allocated per sound this frame
allocatedPerSound := make(map[string]int)

for _, cand := range allowed {
soundID := cand.source.SoundID
pool, exists := sys.Manager.voices[soundID]
if !exists {
continue
}
if allocatedPerSound[soundID] < len(pool) {
awardedVoices[cand.id] = cand
allocatedPerSound[soundID]++
}
}

// 4. Update the actual Hardware Voices
for soundID, pool := range sys.Manager.voices {
for i := range pool {
voice := &pool[i]

// If this voice has an owner from last frame, check if they still get to play
if voice.Owner != 0 {
cand, getsVoice := awardedVoices[voice.Owner]
if getsVoice {
// Update volume & panning
sys.updateVoiceState(voice, cand, anchorPos)
// Remove from map so we know it's handled
					delete(awardedVoices, cand.id)
				} else {
					// Entity stopped playing or moved out of chunk limits
					rl.StopSound(voice.HardwareSound)
					voice.IsPlaying = false
					voice.Owner = 0
				}
			}
		}
		
		// Map remaining unassigned candidates to free voices in the pool
		for candID, cand := range awardedVoices {
			if cand.source.SoundID != soundID {
				continue
			}
			// Find free voice
			for i := range pool {
				voice := &pool[i]
				if voice.Owner == 0 {
					voice.Owner = candID
					sys.updateVoiceState(voice, cand, anchorPos)
					break
				}
			}
		}
	}
}

func (sys SpatialAudioSystem) updateVoiceState(voice *AudioVoice, cand audioCandidate, anchorPos Position3D) {
	// Attenuation
	vol := cand.source.Volume * (1.0 - (cand.dist / cand.source.MaxDistance))
	if vol < 0 {
		vol = 0
	}

	// Simple 3D panning mapped to 2D Stereo (Right relative to anchor assuming looking down -Z)
	// If looking down -Z, the "Right" vector is (1, 0, 0)
	dirX := float32(0.0)
	if cand.dist > 0.01 {
		dirX = (cand.pos.X - anchorPos.X) / cand.dist
	}
	pan := 0.5 + (dirX * 0.5) // Maps -1..1 to 0..1

	rl.SetSoundVolume(voice.HardwareSound, vol)
	rl.SetSoundPan(voice.HardwareSound, pan)

	if !voice.IsPlaying {
		rl.PlaySound(voice.HardwareSound)
		voice.IsPlaying = true
	} else if !cand.source.IsLooping && !rl.IsSoundPlaying(voice.HardwareSound) {
		// It's a one-shot or non-looping that finished
voice.Owner = 0
voice.IsPlaying = false
} else if cand.source.IsLooping && !rl.IsSoundPlaying(voice.HardwareSound) {
// Restart loop
rl.PlaySound(voice.HardwareSound)
}
}
