package systems

import (
	"math"
	"sort"

	rl "github.com/gen2brain/raylib-go/raylib"

	"rts-go/components"
	"rts-go/ecs"
)

// AudioVoice tracks a hardware playback channel for a specific sound.
type AudioVoice struct {
	HardwareSound rl.Sound
	Owner         ecs.EntityID // 0 if free
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
func (SpatialAudioSystem) Phase() ecs.Phase { return ecs.PhasePostPhysics }

func (SpatialAudioSystem) LODPolicy() ecs.LODPolicy {
	return ecs.LODPolicy{
		ActiveEvery:   0, // Smooth panning/volume changes in near-field
		RelevantEvery: ecs.LODDisabled,
		DormantEvery:  ecs.LODDisabled,
	}
}

func (SpatialAudioSystem) Reads() []ecs.ComponentType {
	return []ecs.ComponentType{ecs.TypeOf[components.Position3D](), ecs.TypeOf[components.AudioSource](), ecs.TypeOf[components.LODAnchor]()}
}
func (SpatialAudioSystem) Writes() []ecs.ComponentType {
	return []ecs.ComponentType{ecs.TypeOf[components.AudioSource]()}
}

// audioCandidate groups entity info for spatial filtering
type audioCandidate struct {
	id     ecs.EntityID
	pos    components.Position3D
	dist   float32
	source components.AudioSource
}

func (sys SpatialAudioSystem) Update(ctx ecs.UpdateContext) {
	var anchorID ecs.EntityID
	var anchorPos components.Position3D
	found := false

	ecs.ForEach2[components.LODAnchor, components.Position3D](ctx.State, func(id ecs.EntityID, _ *components.LODAnchor, pos *components.Position3D) {
		if !found {
			anchorPos = *pos
			anchorID = id
			found = true
		}
	})
	if !found {
		return
	}

	// 1. Collect all playing audio candidates grouped by SoundID and ChunkID
	chunks := make(map[ecs.ChunkID]map[string][]audioCandidate)

	ecs.ForEach3LOD[components.Position3D, components.AudioSource, ecs.LOD](ctx.State, ctx.LOD, func(id ecs.EntityID, pos *components.Position3D, source *components.AudioSource, _ *ecs.LOD) {
		if id == anchorID {
			return
		}

		if !source.IsPlaying {
			return
		}

		dx := pos.X - anchorPos.X
		dy := pos.Y - anchorPos.Y
		dz := pos.Z - anchorPos.Z
		dist := float32(math.Sqrt(float64(dx*dx + dy*dy + dz*dz)))

		if dist > source.MaxDistance {
			return
		}

		chunk := ctx.State.Grid.PosToChunk(pos.X, pos.Y, pos.Z)
		if chunks[chunk] == nil {
			chunks[chunk] = make(map[string][]audioCandidate)
		}
		chunks[chunk][source.SoundID] = append(chunks[chunk][source.SoundID], audioCandidate{
			id: id, pos: *pos, dist: dist, source: *source,
		})
	})

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

	// 3. Allocate hardware voices globally by distance
	sort.Slice(allowed, func(i, j int) bool {
		return allowed[i].dist < allowed[j].dist
	})

	awardedVoices := make(map[ecs.EntityID]audioCandidate)
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

			if voice.Owner != 0 {
				cand, getsVoice := awardedVoices[voice.Owner]
				if getsVoice {
					sys.updateVoiceState(voice, cand, anchorPos)
					delete(awardedVoices, cand.id)
				} else {
					rl.StopSound(voice.HardwareSound)
					voice.IsPlaying = false
					voice.Owner = 0
				}
			}
		}

		for candID, cand := range awardedVoices {
			if cand.source.SoundID != soundID {
				continue
			}
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

func (sys SpatialAudioSystem) updateVoiceState(voice *AudioVoice, cand audioCandidate, anchorPos components.Position3D) {
	vol := cand.source.Volume * (1.0 - (cand.dist / cand.source.MaxDistance))
	if vol < 0 {
		vol = 0
	}

	dirX := float32(0.0)
	if cand.dist > 0.01 {
		dirX = (cand.pos.X - anchorPos.X) / cand.dist
	}
	pan := 0.5 + (dirX * 0.5)

	rl.SetSoundVolume(voice.HardwareSound, vol)
	rl.SetSoundPan(voice.HardwareSound, pan)

	if !voice.IsPlaying {
		rl.PlaySound(voice.HardwareSound)
		voice.IsPlaying = true
	} else if !cand.source.IsLooping && !rl.IsSoundPlaying(voice.HardwareSound) {
		voice.Owner = 0
		voice.IsPlaying = false
	} else if cand.source.IsLooping && !rl.IsSoundPlaying(voice.HardwareSound) {
		rl.PlaySound(voice.HardwareSound)
	}
}
