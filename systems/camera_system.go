package systems

import (
	"rts-go/components"
	"rts-go/core"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"
)

// CurrentCamera is the rl.Camera3D that the renderer should use. CameraSystem
// updates this each frame based on ECS Camera/Transform state.
var CurrentCamera rl.Camera3D

// CurrentOriginChunk is the chunk that the render space is anchored to this
// frame. Every drawn entity's WorldPos must be projected through
// WorldPos.ToRenderSpace(CurrentOriginChunk) before being passed to raylib.
// Updated together with CurrentCamera by CameraSystem.
var CurrentOriginChunk components.ChunkCoord

// CameraSystem synchronizes ECS camera components to a raylib Camera3D.
type CameraSystem struct {
	camFilter *ecs.Filter2[components.Camera, components.WorldPos]
	activeMap *ecs.Map[components.ActiveCamera]
	orbitMap  *ecs.Map[components.OrbitController]
	posMap    *ecs.Map[components.WorldPos]
}

func (sys *CameraSystem) InitUI(w *ecs.World) {
	sys.camFilter = ecs.NewFilter2[components.Camera, components.WorldPos](w)
	sys.activeMap = ecs.NewMap[components.ActiveCamera](w)
	sys.orbitMap = ecs.NewMap[components.OrbitController](w)
	sys.posMap = ecs.NewMap[components.WorldPos](w)
}

func (CameraSystem) Name() string { return "camera" }

func (CameraSystem) LODPolicy() core.LODPolicy {
	return core.LODPolicy{ActiveEvery: 0, RelevantEvery: core.LODDisabled, DormantEvery: core.LODDisabled}
}

func (sys CameraSystem) Update(ctx core.UpdateContext) {
	// Find active camera (prefer entity with ActiveCamera marker)
	q := sys.camFilter.Query()
	var found bool
	for q.Next() {
		cam, camPos := q.Get()
		id := q.Entity()
		if sys.activeMap.Has(id) || !found {
			// Determine target: if entity also has an OrbitController, use its target pos.
			var targetPos components.WorldPos
			if orb := sys.orbitMap.Get(id); orb != nil {
				tptr := sys.posMap.Get(orb.Target)
				if tptr != nil {
					targetPos = *tptr
				}
			}

			// Render origin = camera's chunk. The camera itself sits at
			// camPos.Local in render space; the target is projected relative
			// to that origin.
			CurrentOriginChunk = camPos.Chunk
			CurrentCamera = rl.Camera3D{
				Position:   camPos.Local,
				Target:     targetPos.ToRenderSpace(camPos.Chunk),
				Up:         rl.Vector3{X: 0, Y: 1, Z: 0},
				Fovy:       cam.Fovy,
				Projection: rl.CameraPerspective,
			}
			found = true
			// prefer the first ActiveCamera found; if multiple, first wins
			if sys.activeMap.Has(id) {
				// Close query before breaking to release world lock
				q.Close()
				break
			}
		}
	}
}
