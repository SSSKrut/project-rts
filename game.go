package main

import (
	"time"

	rl "github.com/gen2brain/raylib-go/raylib"

	"rts-go/components"
	"rts-go/core"
	"rts-go/entities"
	"rts-go/systems"
	"rts-go/ui"

	"github.com/mlange-42/ark/ecs"
)

// Game owns what used to be the locals of main(). Allocated once and NEVER
// copied: world resources are registered by address (`ecs.AddResource(world,
// &g.Res.X)`), so a copy would leave the world pointing into the original.
type Game struct {
	App   *core.App
	World *ecs.World

	Res   worldRes
	Svc   gameServices
	Maps  gameMaps
	Filt  gameFilters
	Ctx   renderCtx
	Scene sceneHarness
	UI    uiState
	Sel   selection
	Dev   devState
	Frame frameState

	anchor ecs.Entity
	camEnt ecs.Entity

	terrainMaterial rl.Material
	hudFont         rl.Font
	hudFontIsCustom bool
	headless        bool
	shotDone        bool
	workerPool      *core.WorkerPool
	saveDir         string
}

// worldRes holds the singleton resources. Every field here is handed to
// ecs.AddResource by address — see the no-copy rule on Game.
type worldRes struct {
	Streaming        components.StreamingMap
	TerrainIndex     systems.TerrainChunkIndex
	PropRegistry     *components.PropTypeRegistry
	PropIndex        systems.PropChunkIndex
	Rivers           components.Rivers
	RoadGraph        components.RoadGraph
	RoadSurface      components.RoadSurface
	BridgeEdges      int
	BuildingPlans    components.BuildingPlanList
	BuildingIndex    systems.BuildingChildIndex
	BuildingPlanIx   systems.BuildingPlanIndex
	Trenches         components.TrenchNetwork
	CoverSlotIndex   systems.CoverSlotIndex
	Transitions      components.TransitionRegistry
	MapMarkerCache   components.MapMarkerCache
	FormationPresets components.FormationPresets
	UnitHash         *core.SpatialHash
	VehicleHash      *core.VehicleSpatialHash
	EventLog         *components.EventLog
	OrderHistory     *components.OrderHistory
	ContactRegistry  components.ContactRegistry
	Symbology        components.SymbologyPresets
	Atmosphere       components.Atmosphere
	DayClock         components.DayClock
}

type gameServices struct {
	Stamper        *systems.Stamper
	Nav            *systems.NavService
	Squad          *systems.SquadService
	Damage         *systems.DamageService
	MapPing        *systems.MapPingService
	Role           *systems.RoleService
	UnitFactory    *entities.UnitFactory
	VehicleFactory *entities.VehicleFactory
	// AircraftFactory is built with the other gameplay handles, LATE, so the
	// aircraft component types take IDs after every existing one. Ark assigns
	// IDs on first registration and the ID order decides archetype iteration
	// order, which decides float accumulation order in the movement passes —
	// registering these early would drift every existing scene's replay hash
	// for no reason.
	AircraftFactory *entities.AircraftFactory
	// AirTraffic is held so the factory can be injected after construction:
	// the system is registered in registerSystems, which runs before the
	// factory exists.
	AirTraffic *systems.AirTrafficSystem
}

// gameMaps holds every ecs.Map handle the UI / input / render halves read.
// Built once — resolving an archetype per frame would allocate.
type gameMaps struct {
	Pos                  *ecs.Map[components.WorldPos]
	LODActive            *ecs.Map[components.LODActive]
	LODAnchor            *ecs.Map[components.LODAnchor]
	AlwaysActive         *ecs.Map[components.AlwaysActive]
	Camera               *ecs.Map[components.Camera]
	Orbit                *ecs.Map[components.OrbitController]
	ActiveCam            *ecs.Map[components.ActiveCamera]
	Building             *ecs.Map[components.Building]
	BuildingMember       *ecs.Map[components.BuildingMember]
	Level                *ecs.Map[components.Level]
	BuildingViewMode     *ecs.Map[components.BuildingViewMode]
	LevelVisibility      *ecs.Map[components.LevelVisibility]
	TrenchRoot           *ecs.Map[components.TrenchRoot]
	Role                 *ecs.Map[components.UnitRole]
	SquadMember          *ecs.Map[components.SquadMember]
	Roster               *ecs.Map[components.CommandRoster]
	FormationData        *ecs.Map[components.FormationData]
	FormationOrient      *ecs.Map[components.FormationOrientation]
	FormationCustomSlots *ecs.Map[components.FormationCustomSlots]
	OrderQueue           *ecs.Map[components.OrderQueueHead]
	OrderKind            *ecs.Map[components.OrderKind]
	OrderTarget          *ecs.Map[components.OrderTarget]
	OrderChain           *ecs.Map[components.OrderChain]
	OrderState           *ecs.Map[components.OrderState]
	OrderProgress        *ecs.Map[components.OrderProgress]
	OrderIssuedAt        *ecs.Map[components.OrderIssuedAt]
	MovementProfile      *ecs.Map[components.MovementProfile]
	Stamina              *ecs.Map[components.Stamina]
	HP                   *ecs.Map[components.HP]
	Faction              *ecs.Map[components.Faction]
	Controller           *ecs.Map[components.Controller]
	Detectability        *ecs.Map[components.Detectability]
	CirclePatrol         *ecs.Map[components.CirclePatrol]
	IndividualPos        *ecs.Map[components.IndividualPosition]
	Turret               *ecs.Map[components.Turret]
	LevelMember          *ecs.Map[components.LevelMember]
	CoverDirRead         *ecs.Map[components.CoverDirection]
	LevelVisRead         *ecs.Map[components.LevelVisibility]
	Contact              *ecs.Map[components.Contact]
	ContactOverride      *ecs.Map[components.ContactSymbolOverride]
	ContactPlayerSet     *ecs.Map[components.ContactPlayerSet]
	UnitOverride         *ecs.Map[components.UnitSymbolOverride]
	SquadOverride        *ecs.Map[components.SquadSymbolOverride]
	Vehicle              *ecs.Map[components.Vehicle]
	Aircraft             *ecs.Map[components.Aircraft]
	SquadMarker          *ecs.Map[components.Squad]
	ActionQueue          *ecs.Map[components.ActionQueue]
}

// gameFilters holds the query handles used outside the systems.
type gameFilters struct {
	UnitRender     *ecs.Filter3[components.WorldPos, components.Unit, components.Stance]
	VehicleRender  *ecs.Filter2[components.WorldPos, components.Vehicle]
	AircraftRender *ecs.Filter2[components.WorldPos, components.Aircraft]
	SmokeRender    *ecs.Filter2[components.WorldPos, components.SmokeField]
	ChunkActive    *ecs.Filter3[components.WorldPos, components.ChunkMesh, components.LODActive]
	ChunkRelevant  *ecs.Filter3[components.WorldPos, components.ChunkMesh, components.LODRelevant]
	Prop           *ecs.Filter2[components.WorldPos, components.Prop]
	WallRender     *ecs.Filter2[components.WorldPos, components.WallSegment]
	FloorRender    *ecs.Filter2[components.WorldPos, components.Floor]
	StairsRender   *ecs.Filter2[components.WorldPos, components.Stairs]
	RoofRender     *ecs.Filter2[components.WorldPos, components.Roof]
	LevelCutaway   *ecs.Filter2[components.Level, components.BuildingMember]
	NavOverlay     *ecs.Filter4[components.WorldPos, components.ChunkCoord, components.NavGrid, components.Heightmap]
	CoverOverlay   *ecs.Filter4[components.WorldPos, components.ChunkCoord, components.CoverMap, components.Heightmap]
	CoverSlot      *ecs.Filter2[components.WorldPos, components.CoverSlot]
	MapPing        *ecs.Filter2[components.WorldPos, components.MapPing]
	NavGridChunk   *ecs.Filter1[components.NavGrid]
	FloorNav       *ecs.Filter3[components.WorldPos, components.Level, components.LevelNavGrid]
	VisionAware    *ecs.Filter2[components.WorldPos, components.Awareness]
	UnitPathSquad  *ecs.Filter4[components.Unit, components.WorldPos, components.MicroPath, components.SquadMember]
	UnitPathSolo   *ecs.Filter3[components.Unit, components.WorldPos, components.MicroPath]
	ChunkAll       *ecs.Filter1[components.TerrainChunk]
	Weapon         *ecs.Filter1[components.Weapon]
	Unit           *ecs.Filter1[components.Unit]
	StairsCount    *ecs.Filter1[components.Stairs]
	Squad          *ecs.Filter2[components.Squad, components.CommandRoster]
	// Commander: squads AND soloists — anything that owns an order queue.
	Commander  *ecs.Filter2[components.CommandRoster, components.OrderQueueHead]
	Contact    *ecs.Filter1[components.Contact]
	Building   *ecs.Filter1[components.Building]
	TrenchRoot *ecs.Filter1[components.TrenchRoot]
	UnitHit    *ecs.Filter2[components.Unit, components.WorldPos]
}

// renderCtx bundles the purpose-built context structs the draw helpers take.
type renderCtx struct {
	LOS         *losPreviewState
	Inspector   ui.InspectorMaps
	Behavior    ui.BehaviorMaps
	HitTest     *HitTester
	Ghost       *ghostContext
	Route       *routePreviewCtx
	OrderMarker orderMarkerCtx
	Particle    ParticleRenderCtx
	Ribbons     ribbonSet
	WorldShader *worldShader
	Models      *modelSet
	Exhaust     *exhaustField
	Clouds      *cloudRenderer
	Pyramid     *systems.HeightPyramid
	FarTerrain  *farTerrain
	CloudDrift  rl.Vector2
	Daylight    skyPalette
}

// sceneHarness holds the scripted test scenes (-scene=...); nil in normal play.
type sceneHarness struct {
	Door *doorSceneState
	AI   *aiTestState
}

// Selection / order input gates on Controller, not Faction (DP-4).
func (g *Game) isControllable(ent ecs.Entity) bool {
	c := g.Maps.Controller.Get(ent)
	return c != nil && c.Owner == components.ControllerLocal
}

// Missing Faction (legacy spawns) falls through to FactionPlayer.
func (g *Game) squadColor(ent ecs.Entity) rl.Color {
	faction := components.FactionPlayer
	if ent != (ecs.Entity{}) && g.World.Alive(ent) {
		if f := g.Maps.Faction.Get(ent); f != nil {
			faction = f.ID
		}
	}
	return squadColorFor(ent, faction)
}

// unitFactory keeps the func(WorldPos) ecs.Entity shape the spawners expect.
func (g *Game) unitFactory(p components.WorldPos) ecs.Entity {
	return g.Svc.UnitFactory.Spawn(p)
}

// uiState is the panel / widget state that persists across frames.
type uiState struct {
	PanelMgr  *ui.PanelManager
	Scene3DRT *ui.Scene3DRT
	Underlay  ui.MapUnderlay
	MapCam    ui.MapCamera
	ScreenW   int32
	ScreenH   int32

	MapPanning   bool
	MapPanCursor rl.Vector2
	Ruler        rulerState

	CtxMenu           ui.ContextMenu
	ContactCtxMenu    ui.ContextMenu
	ContactMenuTarget ecs.Entity
	ChevronMenu       ui.ChevronMenu
	Floating          *ui.FloatingState

	RMB            rmbSession
	LastRMBPressAt float32

	// Which surface's scrollbar thumb is being dragged ("" = none): a
	// workspace leaf keys on PanelID, a floater on its own ID.
	ScrollDragKey    string
	ScrollDragStartY float32
	ScrollDragStartO float32
	// SquadBar remembers card order + tombstones across frames.
	SquadBar *ui.SquadBarState
	// ScrollSurf is the per-frame surface list, retained to avoid a
	// per-frame allocation.
	ScrollSurf []scrollSurface

	MarqueeStart  rl.Vector2
	MarqueeActive bool
	MarqueeOrigin ui.PanelID

	// One view per surface: the same widget open as a leaf and as a floater
	// has two widths, so it needs two offsets, two zooms and two gutters.
	// Keyed like ScrollDragKey — PanelID for a leaf, floater ID otherwise.
	TimelineViews map[string]*ui.TimelineViewState
	TimelineSurf  []timelineSurface
	TimelineData  ui.TimelineData
	// Hover belongs to whichever surface the cursor is over — only one can be.
	TimelineHoverHit ui.TimelineHit
	TimelineHoverOK  bool
	TimelineHoverBlk ui.TimelineOrderBlock
	TimelineDragKey  string
	TimelineDragKind timelineDragKind
	TimelineDragOff  float32
	TopBarHits       ui.TopBarHits

	FormationEditor *ui.FormationEditor
	SymbolEditor    *ui.SymbolEditor
	BuildingWidget  *ui.BuildingWidgetLayout

	SmoothedSquadPos map[ecs.Entity]components.WorldPos
	ExpandedHUD      bool
	ShowMapDebugLy   bool

	// Attention holds the auto-reaction policy plus the live banner; it is
	// frame-side state and never enters a world snapshot.
	Attention attentionState
	Cues      audioCues
	// SpecSubject is what the spec card currently shows; the card is a view
	// on the spec tables, so one subject serves every surface showing it.
	SpecSubject ui.SpecSubject
}

// rmbSession bundles RMB-hold state. Active is set on press and cleared on
// release; PressOrigin / PressTarget are captured at press time so a release
// commit does not drift with the cursor.
type rmbSession struct {
	Active      bool
	SourcePanel ui.PanelID
	PressOrigin rl.Vector2
	PressTarget components.WorldPos
	// PressRaw keeps the unsnapped cursor point (PressTarget snaps to the
	// ground-floor centre over a building) — room picking needs the real XZ.
	PressRaw     components.WorldPos
	PressTimeSec float32
	HoveredBldg  ecs.Entity
	HasSelection bool
	FacingActive bool // > 8 px drag committed to facing-drag
	Ctrl, Alt    bool
	Double       bool
}

// selection is what the player currently has picked or is hovering.
type selection struct {
	Units            []ecs.Entity
	Hovered          ecs.Entity
	HoveredBuilding  ecs.Entity
	HoveredLevel     ecs.Entity
	Building         ecs.Entity // sticky pin: survives the cursor leaving the footprint
	NavPath          []components.WorldPos
	Binds            [5]bindEntry
	LastMapClickEnt  ecs.Entity
	LastMapClickTime float32
	// Capture-only: -shot-order issues its route once.
	OrderShot     bool
	OrderShotLegs int
}

// devState backs the Debug widget: single-step, spawn palette, recovered panic.
type devState struct {
	SpawnKind    int // 0 off; 1 rifleman; 2 enemy; 3..7 vehicle kinds
	PendingSteps int
	Fatal        *simFatalState
}

const (
	// 300 ms window for double-RMB — wide enough for relaxed chains, narrow
	// enough that two deliberate sequential clicks don't fuse.
	rmbDoubleWindow  float32 = 0.30
	wheelScrollSpeed float32 = 30
	// One notch scrolls two squad rows in the timeline's list.
	timelineWheelRows     float32 = 64
	marqueeClickThreshold float32 = 5
)

// frameState is the per-frame scratch that crosses phase boundaries. Reset
// implicitly: every field is written before it is read within a frame.
type frameState struct {
	DtReal  time.Duration
	Cursor  rl.Vector2
	Focused ui.PanelID
	Shift   bool
	Ctrl    bool
	Alt     bool

	Panel3D         ui.Panel
	PanelMap        ui.Panel
	Panel3DContent  rl.Rectangle
	PanelMapContent rl.Rectangle
	Panel3DLocal    rl.Vector2
	Panel3DW        int32
	Panel3DH        int32

	LMBDown  bool
	LMBPress bool

	// AnchorPos points into ECS storage and is re-fetched after the sim tick:
	// an archetype change during Advance can move the component.
	AnchorPos   *components.WorldPos
	AnchorSpeed float32
	WASDActive  bool

	GhostTarget   components.WorldPos
	GhostTargetOK bool

	// SquadBar is laid out before input so world clicks can be vetoed over
	// it in the same frame, then drawn from this frozen list after the tick.
	SquadBar ui.BarLayout

	// Contact formations for the map, rebuilt once per frame after the tick.
	// Input runs before the next Advance, so the set the pick path reads is
	// exactly the one that was drawn.
	MapClusters ui.ContactClusterSet

	// Counters accumulated by drawScene3D, consumed by drawUI's census.
	ChunksActive int
	ChunksRel    int
	UnitsLive    int
	PropsLive    int
	RibbonsDrawn int
	FloorsLive   int
	WallsLive    int
	VisionPairs  int
	CoverSlots   int
	SquadsLive   int
	SquadMembers int
}
