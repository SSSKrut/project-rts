package systems

import (
	"rts-go/components"
	"rts-go/core"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"
)

var CurrentCamera rl.Camera3D

// CurrentOriginChunk anchors the render space this frame. Every drawn entity
// must project its WorldPos via ToRenderSpace(CurrentOriginChunk) so float32
// precision stays bounded near the camera no matter how far the anchor has
// travelled.
var CurrentOriginChunk components.ChunkCoord

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
	q := sys.camFilter.Query()
	var found bool
	for q.Next() {
		cam, camPos := q.Get()
		id := q.Entity()
		if sys.activeMap.Has(id) || !found {
			var targetPos components.WorldPos
			if orb := sys.orbitMap.Get(id); orb != nil {
				tptr := sys.posMap.Get(orb.Target)
				if tptr != nil {
					targetPos = *tptr
				}
			}

			CurrentOriginChunk = camPos.Chunk
			CurrentCamera = rl.Camera3D{
				Position:   camPos.Local,
				Target:     targetPos.ToRenderSpace(camPos.Chunk),
				Up:         rl.Vector3{X: 0, Y: 1, Z: 0},
				Fovy:       cam.Fovy,
				Projection: rl.CameraPerspective,
			}
			found = true
			if sys.activeMap.Has(id) {
				q.Close()
				break
			}
		}
	}
}
