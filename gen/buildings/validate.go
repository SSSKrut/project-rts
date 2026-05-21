package buildings

import (
	"fmt"

	"rts-go/components"
)

// Severity ranks ValidationIssue. Errors block loader acceptance; warnings
// are logged and the plan still loads.
type Severity uint8

const (
	SeverityWarning Severity = iota
	SeverityError
)

func (s Severity) String() string {
	switch s {
	case SeverityError:
		return "ERROR"
	default:
		return "WARN"
	}
}

// ValidationCode enumerates plan-shape problems Validate can detect.
type ValidationCode uint8

const (
	CodeEmptyLevels ValidationCode = iota
	CodeBadLevelRef
	CodeEmptyLevel
	CodeLevelOverlap
	CodeOrphanDoor
	CodeStairWpUnanchored
	CodeSlotOutsideHost
	CodeLevelVolumeMissing
)

func (c ValidationCode) String() string {
	switch c {
	case CodeEmptyLevels:
		return "EmptyLevels"
	case CodeBadLevelRef:
		return "BadLevelRef"
	case CodeEmptyLevel:
		return "EmptyLevel"
	case CodeLevelOverlap:
		return "LevelOverlap"
	case CodeOrphanDoor:
		return "OrphanDoor"
	case CodeStairWpUnanchored:
		return "StairWpUnanchored"
	case CodeSlotOutsideHost:
		return "SlotOutsideHost"
	case CodeLevelVolumeMissing:
		return "LevelVolumeMissing"
	default:
		return fmt.Sprintf("Code(%d)", c)
	}
}

// ValidationIssue is one diagnostic. EntityIdx is the slice index of the
// offending entity (Floor / Wall / Stair / Transition / Furniture /
// Marker / Level) - sandbox uses it for click-to-fly camera. -1 when N/A.
type ValidationIssue struct {
	Severity  Severity
	Code      ValidationCode
	Message   string
	EntityIdx int
}

func (iss ValidationIssue) String() string {
	return fmt.Sprintf("[%s] %s: %s", iss.Severity, iss.Code, iss.Message)
}

// Validate runs structural checks on a BuildingPlan. Empty result = plan
// is well-formed. Both Phase 16.5 generator and Phase 16.A .glb loader
// call this on their output; loader rejects on any Error.
func Validate(plan *components.BuildingPlan) []ValidationIssue {
	var issues []ValidationIssue

	nLevels := len(plan.Levels)
	if nLevels == 0 {
		issues = append(issues, ValidationIssue{
			Severity:  SeverityError,
			Code:      CodeEmptyLevels,
			Message:   "BuildingPlan has no Levels",
			EntityIdx: -1,
		})
		return issues
	}

	checkLevelRef := func(ref uint8, what string, idx int) {
		if ref == components.NoLevelRef {
			return
		}
		if int(ref) >= nLevels {
			issues = append(issues, ValidationIssue{
				Severity:  SeverityError,
				Code:      CodeBadLevelRef,
				Message:   fmt.Sprintf("%s[%d] LevelRef=%d out of range (have %d levels)", what, idx, ref, nLevels),
				EntityIdx: idx,
			})
		}
	}

	levelHasFloor := make([]bool, nLevels)
	for i, f := range plan.Floors {
		checkLevelRef(f.LevelRef, "Floor", i)
		if f.LevelRef != components.NoLevelRef && int(f.LevelRef) < nLevels {
			levelHasFloor[f.LevelRef] = true
		}
	}
	for i, has := range levelHasFloor {
		if !has {
			issues = append(issues, ValidationIssue{
				Severity:  SeverityWarning,
				Code:      CodeEmptyLevel,
				Message:   fmt.Sprintf("Level[%d] %q has no Floor", i, plan.Levels[i].Name),
				EntityIdx: i,
			})
		}
	}

	for i, w := range plan.Walls {
		if len(w.LevelRefs) == 0 {
			issues = append(issues, ValidationIssue{
				Severity:  SeverityWarning,
				Code:      CodeLevelVolumeMissing,
				Message:   fmt.Sprintf("Wall[%d] has no LevelRefs", i),
				EntityIdx: i,
			})
		}
		for _, ref := range w.LevelRefs {
			checkLevelRef(ref, "Wall", i)
		}
	}

	for i, f := range plan.Furniture {
		checkLevelRef(f.LevelRef, "Furniture", i)
	}
	for i, m := range plan.Markers {
		checkLevelRef(m.LevelRef, "Marker", i)
	}

	for i, s := range plan.Stairs {
		if len(s.Waypoints) < 2 {
			issues = append(issues, ValidationIssue{
				Severity:  SeverityError,
				Code:      CodeStairWpUnanchored,
				Message:   fmt.Sprintf("Stair[%d] needs >=2 waypoints (have %d)", i, len(s.Waypoints)),
				EntityIdx: i,
			})
			continue
		}
		lastWp := uint8(len(s.Waypoints) - 1)
		hasStart, hasEnd := false, false
		for _, a := range s.Anchors {
			checkLevelRef(a.LevelRef, "StairAnchor", i)
			if a.WpIndex == 0 {
				hasStart = true
			}
			if a.WpIndex == lastWp {
				hasEnd = true
			}
		}
		if !hasStart || !hasEnd {
			issues = append(issues, ValidationIssue{
				Severity:  SeverityError,
				Code:      CodeStairWpUnanchored,
				Message:   fmt.Sprintf("Stair[%d] missing level anchor at endpoint(s) (start=%v end=%v)", i, hasStart, hasEnd),
				EntityIdx: i,
			})
		}
	}

	for i, t := range plan.LevelTransitions {
		if int(t.LevelA) >= nLevels || int(t.LevelB) >= nLevels {
			issues = append(issues, ValidationIssue{
				Severity:  SeverityError,
				Code:      CodeOrphanDoor,
				Message:   fmt.Sprintf("Transition[%d] LevelA=%d LevelB=%d out of range", i, t.LevelA, t.LevelB),
				EntityIdx: i,
			})
		}
		if t.ViaWall >= 0 && int(t.ViaWall) >= len(plan.Walls) {
			issues = append(issues, ValidationIssue{
				Severity:  SeverityError,
				Code:      CodeOrphanDoor,
				Message:   fmt.Sprintf("Transition[%d] ViaWall=%d out of range (have %d walls)", i, t.ViaWall, len(plan.Walls)),
				EntityIdx: i,
			})
		}
	}

	const eps float32 = 0.1
	for i := 0; i < nLevels; i++ {
		for j := i + 1; j < nLevels; j++ {
			a := plan.Levels[i].AABB
			b := plan.Levels[j].AABB
			if overlapXZ(a, b, eps) && overlapY(a, b, eps) {
				issues = append(issues, ValidationIssue{
					Severity:  SeverityError,
					Code:      CodeLevelOverlap,
					Message:   fmt.Sprintf("Level[%d] %q overlaps Level[%d] %q in XZ+Y", i, plan.Levels[i].Name, j, plan.Levels[j].Name),
					EntityIdx: i,
				})
			}
		}
	}

	return issues
}

func overlapXZ(a, b components.AABB3D, eps float32) bool {
	return a.MinX < b.MaxX-eps && a.MaxX > b.MinX+eps &&
		a.MinZ < b.MaxZ-eps && a.MaxZ > b.MinZ+eps
}

func overlapY(a, b components.AABB3D, eps float32) bool {
	return a.MinY < b.MaxY-eps && a.MaxY > b.MinY+eps
}

// HasErrors returns true when any issue has SeverityError.
func HasErrors(issues []ValidationIssue) bool {
	for _, iss := range issues {
		if iss.Severity == SeverityError {
			return true
		}
	}
	return false
}
