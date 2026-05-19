package components

// DoctrineCode names the macro stance applied to a squad. Doctrine bundles
// MovementProfile + EngagementRules + BehaviorRules defaults; one click swaps
// all three.
type DoctrineCode uint8

const (
	DoctrineNone DoctrineCode = iota
	DoctrinePatrol
	DoctrineAssault
	DoctrineStealth
	DoctrineDefense
)

// ActiveDoctrine marks the doctrine currently applied to a squad. Set by the
// Inspector chip click; the Inspector highlights the active chip. Doctrine
// "writes through" each spec field, so subsequent player edits to any
// individual chip do not toggle the doctrine off automatically (the chip
// just stops matching - see DoctrineMatches).
type ActiveDoctrine struct {
	Code DoctrineCode
}

// DoctrineSpec is one row in the per-doctrine field table. ApplyDoctrine
// returns the spec; Inspector chip click writes spec.Movement / Engagement /
// Behavior into the squad's three components.
type DoctrineSpec struct {
	Movement MovementProfile
	Engage   EngagementRules
	Behavior BehaviorRules
}

// DoctrineSpecs is the spec table per Phase 14.5 pattern. Indexed by
// DoctrineCode. The DoctrineNone row stays zero - no writer should apply it.
var DoctrineSpecs = [5]DoctrineSpec{
	DoctrineNone: {},
	DoctrinePatrol: {
		Movement: MovementProfile{
			Pace: PaceWalk, Stance: StanceStand,
			Posture: PostureStandard, PathStyle: PathStyleRoadPrefer,
		},
		Engage: EngagementRules{
			Mode: FreeFire, FireOnInf: true, FireOnArm: true, FireOnAir: false, FireOnStruct: false,
		},
		Behavior: BehaviorRules{
			AllowAutoReposition: true, AllowAutoStance: true,
			HoldUntilOrdered: false, AllowReturnFire: true,
			SuppressionThreshold: 0.45,
		},
	},
	DoctrineAssault: {
		Movement: MovementProfile{
			Pace: PaceRun, Stance: StanceStand,
			Posture: PostureStandard, PathStyle: PathStyleDirect,
		},
		Engage: EngagementRules{
			Mode: FreeFire, FireOnInf: true, FireOnArm: true, FireOnAir: false, FireOnStruct: true,
		},
		Behavior: BehaviorRules{
			AllowAutoReposition: true, AllowAutoStance: false,
			HoldUntilOrdered: false, AllowReturnFire: true,
			SuppressionThreshold: 0.6,
		},
	},
	DoctrineStealth: {
		Movement: MovementProfile{
			Pace: PaceWalk, Stance: StanceCrouch,
			Posture: PostureQuiet, PathStyle: PathStyleCoverSeek,
		},
		Engage: EngagementRules{
			Mode: HoldFire, FireOnInf: false, FireOnArm: false, FireOnAir: false, FireOnStruct: false,
		},
		Behavior: BehaviorRules{
			AllowAutoReposition: false, AllowAutoStance: true,
			HoldUntilOrdered: true, AllowReturnFire: false,
			SuppressionThreshold: 0.3,
		},
	},
	DoctrineDefense: {
		Movement: MovementProfile{
			Pace: PaceWalk, Stance: StanceCrouch,
			Posture: PostureStandard, PathStyle: PathStyleDirect,
		},
		Engage: EngagementRules{
			Mode: ReturnFire, FireOnInf: true, FireOnArm: true, FireOnAir: false, FireOnStruct: false,
		},
		Behavior: BehaviorRules{
			AllowAutoReposition: false, AllowAutoStance: true,
			HoldUntilOrdered: false, AllowReturnFire: true,
			SuppressionThreshold: 0.4,
		},
	},
}

func DoctrineName(c DoctrineCode) string {
	switch c {
	case DoctrinePatrol:
		return "Patrol"
	case DoctrineAssault:
		return "Assault"
	case DoctrineStealth:
		return "Stealth"
	case DoctrineDefense:
		return "Defense"
	}
	return "-"
}

// DoctrineMatches returns true when the live (mp, er, br) match the
// doctrine spec exactly. Inspector uses it to highlight the active chip;
// any hand-edit to one field flips the chip back to "not active" but does
// not clear ActiveDoctrine - the doctrine stays a record of "what was last
// applied", chip just stops matching.
func DoctrineMatches(c DoctrineCode, mp MovementProfile, er EngagementRules, br BehaviorRules) bool {
	if c == DoctrineNone {
		return false
	}
	spec := DoctrineSpecs[c]
	return mp == spec.Movement && er == spec.Engage && br == spec.Behavior
}
