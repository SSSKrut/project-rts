package systems

import (
	"fmt"
	"reflect"

	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
)

type SavePolicy uint8

const (
	SaveMemcpy SavePolicy = iota
	// SaveSkip: not written to snapshots. Loader restores semantics
	// (ChunkMesh -> zero value + MeshDirty). AudioSource carries a string
	// (placeholder-era audio).
	SaveSkip
)

type saveEntry struct {
	Policy SavePolicy
	LiveID func(w *ecs.World) ecs.ID
}

func saveReg[T any](p SavePolicy) saveEntry {
	return saveEntry{p, func(w *ecs.World) ecs.ID { return ecs.ComponentID[T](w) }}
}

// saveComponents is the classification table: every component type that can
// be registered in the world must have a row. LiveID lets the loader map
// file schema names to this process's lazily-assigned component IDs.
var saveComponents = map[string]saveEntry{
	"components.ChunkMesh":   saveReg[components.ChunkMesh](SaveSkip),
	"components.AudioSource": saveReg[components.AudioSource](SaveSkip),

	"components.WorldPos":   saveReg[components.WorldPos](SaveMemcpy),
	"components.Velocity3D": saveReg[components.Velocity3D](SaveMemcpy),
	"components.ChunkCoord": saveReg[components.ChunkCoord](SaveMemcpy),

	"components.Unit":               saveReg[components.Unit](SaveMemcpy),
	"components.Stance":             saveReg[components.Stance](SaveMemcpy),
	"components.StanceOverride":     saveReg[components.StanceOverride](SaveMemcpy),
	"components.Motion":             saveReg[components.Motion](SaveMemcpy),
	"components.Collider":           saveReg[components.Collider](SaveMemcpy),
	"components.Sensors":            saveReg[components.Sensors](SaveMemcpy),
	"components.Detectability":      saveReg[components.Detectability](SaveMemcpy),
	"components.Awareness":          saveReg[components.Awareness](SaveMemcpy),
	"components.LocalBlackboard":    saveReg[components.LocalBlackboard](SaveMemcpy),
	"components.ActionQueue":        saveReg[components.ActionQueue](SaveMemcpy),
	"components.Equipment":          saveReg[components.Equipment](SaveMemcpy),
	"components.OwnedBy":            saveReg[components.OwnedBy](SaveMemcpy),
	"components.HP":                 saveReg[components.HP](SaveMemcpy),
	"components.Faction":            saveReg[components.Faction](SaveMemcpy),
	"components.Stamina":            saveReg[components.Stamina](SaveMemcpy),
	"components.StaminaExhausted":   saveReg[components.StaminaExhausted](SaveMemcpy),
	"components.UnitRole":           saveReg[components.UnitRole](SaveMemcpy),
	"components.Threat":             saveReg[components.Threat](SaveMemcpy),
	"components.DangerBuffer":       saveReg[components.DangerBuffer](SaveMemcpy),
	"components.MicroPath":          saveReg[components.MicroPath](SaveMemcpy),
	"components.TacticalOverride":   saveReg[components.TacticalOverride](SaveMemcpy),
	"components.CirclePatrol":       saveReg[components.CirclePatrol](SaveMemcpy),
	"components.IndividualPosition": saveReg[components.IndividualPosition](SaveMemcpy),

	"components.Weapon":       saveReg[components.Weapon](SaveMemcpy),
	"components.ThreatSource": saveReg[components.ThreatSource](SaveMemcpy),
	"components.BlastMark":    saveReg[components.BlastMark](SaveMemcpy),
	"components.UnsafeArea":   saveReg[components.UnsafeArea](SaveMemcpy),
	"components.Radio":        saveReg[components.Radio](SaveMemcpy),
	"components.Medkit":       saveReg[components.Medkit](SaveMemcpy),
	"components.Spade":        saveReg[components.Spade](SaveMemcpy),

	"components.Squad":                saveReg[components.Squad](SaveMemcpy),
	"components.SquadMember":          saveReg[components.SquadMember](SaveMemcpy),
	"components.SquadState":           saveReg[components.SquadState](SaveMemcpy),
	"components.SquadPlan":            saveReg[components.SquadPlan](SaveMemcpy),
	"components.CommandRoster":        saveReg[components.CommandRoster](SaveMemcpy),
	"components.FormationData":        saveReg[components.FormationData](SaveMemcpy),
	"components.FormationOrientation": saveReg[components.FormationOrientation](SaveMemcpy),
	"components.FormationCustomSlots": saveReg[components.FormationCustomSlots](SaveMemcpy),
	"components.MacroPath":            saveReg[components.MacroPath](SaveMemcpy),
	"components.CommsState":           saveReg[components.CommsState](SaveMemcpy),
	"components.Relay":                saveReg[components.Relay](SaveMemcpy),
	"components.ControlPoint":         saveReg[components.ControlPoint](SaveMemcpy),
	"components.MovementProfile":      saveReg[components.MovementProfile](SaveMemcpy),
	"components.EngagementRules":      saveReg[components.EngagementRules](SaveMemcpy),
	"components.BehaviorRules":        saveReg[components.BehaviorRules](SaveMemcpy),
	"components.BehaviorRulesEdit":    saveReg[components.BehaviorRulesEdit](SaveMemcpy),
	"components.ActiveDoctrine":       saveReg[components.ActiveDoctrine](SaveMemcpy),
	"components.ActiveAutonomy":       saveReg[components.ActiveAutonomy](SaveMemcpy),

	"components.Order":                        saveReg[components.Order](SaveMemcpy),
	"components.OrderKind":                    saveReg[components.OrderKind](SaveMemcpy),
	"components.OrderState":                   saveReg[components.OrderState](SaveMemcpy),
	"components.OrderOwner":                   saveReg[components.OrderOwner](SaveMemcpy),
	"components.OrderTarget":                  saveReg[components.OrderTarget](SaveMemcpy),
	"components.OrderChain":                   saveReg[components.OrderChain](SaveMemcpy),
	"components.OrderIssuedAt":                saveReg[components.OrderIssuedAt](SaveMemcpy),
	"components.OrderProgress":                saveReg[components.OrderProgress](SaveMemcpy),
	"components.OrderQueueHead":               saveReg[components.OrderQueueHead](SaveMemcpy),
	"components.OrderOutOfRangeTracker":       saveReg[components.OrderOutOfRangeTracker](SaveMemcpy),
	"components.OrderParamFacing":             saveReg[components.OrderParamFacing](SaveMemcpy),
	"components.OrderParamPatrol":             saveReg[components.OrderParamPatrol](SaveMemcpy),
	"components.OrderParamSuppress":           saveReg[components.OrderParamSuppress](SaveMemcpy),
	"components.OrderParamAttackMove":         saveReg[components.OrderParamAttackMove](SaveMemcpy),
	"components.OrderParamMovementProfile":    saveReg[components.OrderParamMovementProfile](SaveMemcpy),
	"components.OrderParamEngagementOverride": saveReg[components.OrderParamEngagementOverride](SaveMemcpy),

	"components.TerrainChunk":   saveReg[components.TerrainChunk](SaveMemcpy),
	"components.Heightmap":      saveReg[components.Heightmap](SaveMemcpy),
	"components.HeightmapDirty": saveReg[components.HeightmapDirty](SaveMemcpy),
	"components.MeshDirty":      saveReg[components.MeshDirty](SaveMemcpy),
	"components.Modified":       saveReg[components.Modified](SaveMemcpy),
	"components.NavGrid":        saveReg[components.NavGrid](SaveMemcpy),
	"components.NavBaked":       saveReg[components.NavBaked](SaveMemcpy),
	"components.CoverMap":       saveReg[components.CoverMap](SaveMemcpy),
	"components.CoverBaked":     saveReg[components.CoverBaked](SaveMemcpy),
	"components.CoverSlot":      saveReg[components.CoverSlot](SaveMemcpy),
	"components.CoverDirection": saveReg[components.CoverDirection](SaveMemcpy),
	"components.ShootingArc":    saveReg[components.ShootingArc](SaveMemcpy),
	"components.Occupancy":      saveReg[components.Occupancy](SaveMemcpy),

	"components.Prop":            saveReg[components.Prop](SaveMemcpy),
	"components.PropsDirty":      saveReg[components.PropsDirty](SaveMemcpy),
	"components.RiverProcessed":  saveReg[components.RiverProcessed](SaveMemcpy),
	"components.RoadProcessed":   saveReg[components.RoadProcessed](SaveMemcpy),
	"components.TrenchProcessed": saveReg[components.TrenchProcessed](SaveMemcpy),
	"components.TrenchRoot":      saveReg[components.TrenchRoot](SaveMemcpy),

	"components.Building":                 saveReg[components.Building](SaveMemcpy),
	"components.BuildingMember":           saveReg[components.BuildingMember](SaveMemcpy),
	"components.BuildingsProcessed":       saveReg[components.BuildingsProcessed](SaveMemcpy),
	"components.BuildingTerrainProcessed": saveReg[components.BuildingTerrainProcessed](SaveMemcpy),
	"components.WallSegment":              saveReg[components.WallSegment](SaveMemcpy),
	"components.Door":                     saveReg[components.Door](SaveMemcpy),
	"components.Window":                   saveReg[components.Window](SaveMemcpy),
	"components.Floor":                    saveReg[components.Floor](SaveMemcpy),
	"components.Roof":                     saveReg[components.Roof](SaveMemcpy),
	"components.Stairs":                   saveReg[components.Stairs](SaveMemcpy),
	"components.StairLevels":              saveReg[components.StairLevels](SaveMemcpy),
	"components.Furniture":                saveReg[components.Furniture](SaveMemcpy),
	"components.Level":                    saveReg[components.Level](SaveMemcpy),
	"components.LevelMember":              saveReg[components.LevelMember](SaveMemcpy),
	"components.LevelNavGrid":             saveReg[components.LevelNavGrid](SaveMemcpy),
	"components.LevelNavBaked":            saveReg[components.LevelNavBaked](SaveMemcpy),
	"components.LevelTransition":          saveReg[components.LevelTransition](SaveMemcpy),
	"components.LevelVisibility":          saveReg[components.LevelVisibility](SaveMemcpy),
	"components.Marker":                   saveReg[components.Marker](SaveMemcpy),

	"components.Contact":               saveReg[components.Contact](SaveMemcpy),
	"components.ContactPlayerSet":      saveReg[components.ContactPlayerSet](SaveMemcpy),
	"components.ContactSymbolOverride": saveReg[components.ContactSymbolOverride](SaveMemcpy),
	"components.UnitSymbolOverride":    saveReg[components.UnitSymbolOverride](SaveMemcpy),
	"components.SquadSymbolOverride":   saveReg[components.SquadSymbolOverride](SaveMemcpy),
	"components.MapPing":               saveReg[components.MapPing](SaveMemcpy),

	"components.Particle":       saveReg[components.Particle](SaveMemcpy),
	"components.ParticleVisual": saveReg[components.ParticleVisual](SaveMemcpy),
	"components.ParticleVel":    saveReg[components.ParticleVel](SaveMemcpy),
	"components.ParticleEnd":    saveReg[components.ParticleEnd](SaveMemcpy),

	"components.LODActive":    saveReg[components.LODActive](SaveMemcpy),
	"components.LODRelevant":  saveReg[components.LODRelevant](SaveMemcpy),
	"components.LODDormant":   saveReg[components.LODDormant](SaveMemcpy),
	"components.LODAnchor":    saveReg[components.LODAnchor](SaveMemcpy),
	"components.AlwaysActive": saveReg[components.AlwaysActive](SaveMemcpy),

	"components.Camera":           saveReg[components.Camera](SaveMemcpy),
	"components.OrbitController":  saveReg[components.OrbitController](SaveMemcpy),
	"components.ActiveCamera":     saveReg[components.ActiveCamera](SaveMemcpy),
	"components.NodeEntity":       saveReg[components.NodeEntity](SaveMemcpy),
	"components.BuildingViewMode": saveReg[components.BuildingViewMode](SaveMemcpy),

	"components.Controller":      saveReg[components.Controller](SaveMemcpy),
	"components.OnGround":        saveReg[components.OnGround](SaveMemcpy),
	"components.Vehicle":         saveReg[components.Vehicle](SaveMemcpy),
	"components.Turret":          saveReg[components.Turret](SaveMemcpy),
	"components.RoadFollower":    saveReg[components.RoadFollower](SaveMemcpy),
	"components.RoadRoute":       saveReg[components.RoadRoute](SaveMemcpy),
	"components.SmokeField":      saveReg[components.SmokeField](SaveMemcpy),
	"components.VehicleOverride": saveReg[components.VehicleOverride](SaveMemcpy),

	"components.Aircraft":         saveReg[components.Aircraft](SaveMemcpy),
	"components.AirArrival":       saveReg[components.AirArrival](SaveMemcpy),
	"components.Missile":          saveReg[components.Missile](SaveMemcpy),
	"components.AircraftOverride": saveReg[components.AircraftOverride](SaveMemcpy),
	"components.AirEngagement":    saveReg[components.AirEngagement](SaveMemcpy),
}

func savePolicyFor(t reflect.Type) (SavePolicy, error) {
	e, ok := saveComponents[t.String()]
	if !ok {
		return 0, fmt.Errorf("save_spec: no policy for component %s", t.String())
	}
	if e.Policy == SaveMemcpy {
		if err := assertPlainData(t, t.String()); err != nil {
			return 0, err
		}
	}
	return e.Policy, nil
}

type ResourceClass uint8

const (
	ResFromManifest ResourceClass = iota // recreated at boot from map manifest / static data
	ResRebuild                           // repopulated from live entities after load
	ResCodec                             // custom serialization in snapshot / sidecar file
)

// resourceClasses covers every ecs.AddResource in main.go. Rebuild notes:
// TerrainChunkIndex/PropChunkIndex/BuildingChildIndex/BuildingPlanIndex/
// CoverSlotIndex/ContactRegistry fill in PostLoadRebuild; TransitionRegistry,
// MapMarkerCache and SpatialHash refresh themselves within the first tick
// (bake Pass 4 runs unconditionally per Active tick).
// RoadGraph becomes ResCodec when destructible bridges land (GAME-VISION M3).
var resourceClasses = map[string]ResourceClass{
	"components.Rivers":           ResFromManifest,
	"components.RoadGraph":        ResFromManifest,
	"components.RoadSurface":      ResFromManifest, // derived from RoadGraph + procgen at boot
	"components.TrenchNetwork":    ResFromManifest,
	"components.BuildingPlanList": ResFromManifest,
	"systems.PropTypeRegistry":    ResFromManifest,

	"systems.TerrainChunkIndex":     ResRebuild,
	"systems.PropChunkIndex":        ResRebuild,
	"systems.BuildingChildIndex":    ResRebuild,
	"systems.BuildingPlanIndex":     ResRebuild,
	"systems.CoverSlotIndex":        ResRebuild,
	"components.TransitionRegistry": ResRebuild,
	"components.MapMarkerCache":     ResRebuild,
	"components.ContactRegistry":    ResRebuild,
	"core.SpatialHash":              ResRebuild,
	"core.VehicleSpatialHash":       ResRebuild,

	"components.StreamingMap": ResFromManifest, // empty since the smart-space demo graph was removed

	"components.EventLog":         ResCodec,
	"components.OrderHistory":     ResCodec,
	"components.FormationPresets": ResCodec,
	"components.SymbologyPresets": ResCodec, // sidecar save/symbols.json (closes deferred 18.5)
}

// VerifySaveSpec is the permanent completeness gate: every component type
// registered in the world must carry a policy. Called on every save.
func VerifySaveSpec(w *ecs.World) error {
	for _, id := range ecs.ComponentIDs(w) {
		info, ok := ecs.ComponentInfo(w, id)
		if !ok {
			continue
		}
		if _, err := savePolicyFor(info.Type); err != nil {
			return err
		}
	}
	return nil
}

func assertPlainData(t reflect.Type, root string) error {
	switch t.Kind() {
	case reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return nil
	case reflect.Array:
		return assertPlainData(t.Elem(), root)
	case reflect.Struct:
		for i := 0; i < t.NumField(); i++ {
			if err := assertPlainData(t.Field(i).Type, root); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("save_spec: %s contains non-plain kind %s", root, t.Kind())
	}
}
