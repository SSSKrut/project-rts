package systems

import (
	"math"
	"sort"

	rl "github.com/gen2brain/raylib-go/raylib"

	"github.com/mlange-42/ark/ecs"
	"rts-go/components"
	"rts-go/core"
)

type AudioVoice struct {
	HardwareSound rl.Sound
	Owner         ecs.Entity // 0 if free
	IsPlaying     bool
	Vol           float32
}

// AudioManager pre-allocates raylib Sound objects to allow independent volume
// and panning for multiple instances of the same loaded Wave.
type AudioManager struct {
	MaxVoicesPerSound int
	voices            map[string][]AudioVoice
}

func NewAudioManager(maxVoices int) *AudioManager {
	return &AudioManager{
		MaxVoicesPerSound: maxVoices,
		voices:            make(map[string][]AudioVoice),
	}
}

// RegisterWave converts one in-memory wave into N independent hardware sounds.
func (am *AudioManager) RegisterWave(id string, wave rl.Wave) {
	pool := make([]AudioVoice, am.MaxVoicesPerSound)
	for i := 0; i < am.MaxVoicesPerSound; i++ {
		snd := rl.LoadSoundFromWave(wave)
		pool[i] = AudioVoice{HardwareSound: snd, Owner: ecs.Entity{}}
	}
	am.voices[id] = pool
}

func (am *AudioManager) Unload() {
	for _, pool := range am.voices {
		for i := range pool {
			rl.UnloadSound(pool[i].HardwareSound)
		}
	}
}

// SpatialAudioSystem limits playing sounds per spatial chunk and maps them to
// hardware voices.
type SpatialAudioSystem struct {
	Manager     *AudioManager
	MaxPerChunk int

	anchorFilter *ecs.Filter2[components.LODAnchor, components.WorldPos]
	activeFilter *ecs.Filter3[components.WorldPos, components.AudioSource, components.LODActive]
	anchorMap    *ecs.Map[components.LODAnchor]
}

func (sys *SpatialAudioSystem) InitUI(w *ecs.World) {
	sys.anchorFilter = ecs.NewFilter2[components.LODAnchor, components.WorldPos](w)
	sys.activeFilter = ecs.NewFilter3[components.WorldPos, components.AudioSource, components.LODActive](w)
	sys.anchorMap = ecs.NewMap[components.LODAnchor](w)
}

func (SpatialAudioSystem) Name() string { return "spatial_audio" }

func (SpatialAudioSystem) LODPolicy() core.LODPolicy {
	return core.LODPolicy{
		ActiveEvery:   0,
		RelevantEvery: core.LODDisabled,
		DormantEvery:  core.LODDisabled,
	}
}

type audioCandidate struct {
	id     ecs.Entity
	pos    components.WorldPos
	delta  rl.Vector3 // anchor -> source
	dist   float32
	source components.AudioSource
}

func (sys SpatialAudioSystem) Update(ctx core.UpdateContext) {
	var anchorID ecs.Entity
	var anchorPos components.WorldPos
	found := false

	q := sys.anchorFilter.Query()
	for q.Next() {
		if !found {
			_, pos := q.Get()
			anchorPos = *pos
			anchorID = q.Entity()
			found = true
		}
	}
	if !found {
		return
	}

	chunks := make(map[[2]int32]map[string][]audioCandidate)

	q2 := sys.activeFilter.Query()
	for q2.Next() {
		id := q2.Entity()
		if id == anchorID {
			continue
		}

		pos, source, _ := q2.Get()
		if !source.IsPlaying {
			continue
		}

		delta := pos.Sub(anchorPos)
		dist := float32(math.Sqrt(float64(delta.X*delta.X + delta.Y*delta.Y + delta.Z*delta.Z)))

		if dist > source.MaxDistance {
			continue
		}

		key := [2]int32{pos.Chunk.X, pos.Chunk.Z}
		if chunks[key] == nil {
			chunks[key] = make(map[string][]audioCandidate)
		}
		chunks[key][source.SoundID] = append(chunks[key][source.SoundID], audioCandidate{
			id: id, pos: *pos, delta: delta, dist: dist, source: *source,
		})
	}

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

	sort.Slice(allowed, func(i, j int) bool {
		return allowed[i].dist < allowed[j].dist
	})

	awardedVoices := make(map[ecs.Entity]audioCandidate)
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

	for soundID, pool := range sys.Manager.voices {
		for i := range pool {
			voice := &pool[i]

			if voice.Owner != (ecs.Entity{}) {
				cand, getsVoice := awardedVoices[voice.Owner]
				if getsVoice {
					sys.updateVoiceState(voice, cand)
					delete(awardedVoices, cand.id)
				} else {
					rl.StopSound(voice.HardwareSound)
					voice.IsPlaying = false
					voice.Owner = ecs.Entity{}
				}
			}
		}

		for candID, cand := range awardedVoices {
			if cand.source.SoundID != soundID {
				continue
			}
			for i := range pool {
				voice := &pool[i]
				if voice.Owner == (ecs.Entity{}) {
					voice.Owner = candID
					sys.updateVoiceState(voice, cand)
					break
				}
			}
		}
	}
}

func (sys SpatialAudioSystem) updateVoiceState(voice *AudioVoice, cand audioCandidate) {
	vol := cand.source.Volume * (1.0 - (cand.dist / cand.source.MaxDistance))
	if vol < 0 {
		vol = 0
	}

	dirX := float32(0.0)
	if cand.dist > 0.01 {
		dirX = cand.delta.X / cand.dist
	}
	pan := 0.5 + (dirX * 0.5)

	rl.SetSoundVolume(voice.HardwareSound, vol)
	rl.SetSoundPan(voice.HardwareSound, pan)

	if !voice.IsPlaying {
		rl.PlaySound(voice.HardwareSound)
		voice.IsPlaying = true
	} else if !cand.source.IsLooping && !rl.IsSoundPlaying(voice.HardwareSound) {
		voice.Owner = ecs.Entity{}
		voice.IsPlaying = false
	} else if cand.source.IsLooping && !rl.IsSoundPlaying(voice.HardwareSound) {
		rl.PlaySound(voice.HardwareSound)
	}
}
