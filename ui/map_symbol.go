package ui

import (
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
)

// squadSymbolHalf is the marker's half-extent in pixels. Sized so a friend
// rectangle (1.5:1) stays inside the old 9 px disc footprint.
const squadSymbolHalf float32 = 7

// squadTintStripH is the squad-colour strip drawn under the frame.
const squadTintStripH float32 = 3

// iconSlots bounds the per-icon tally; IconKind is a small dense enum and the
// tally is indexed by it directly.
const iconSlots = 16

// AffiliationForFaction maps a gameplay faction onto the APP-6 frame shape.
// Unknown factions read as AffilUnknown rather than defaulting to friend.
func AffiliationForFaction(f uint8) components.Affiliation {
	switch f {
	case components.FactionPlayer:
		return components.AffilFriend
	case components.FactionEnemyRed:
		return components.AffilHostile
	case components.FactionNeutral:
		return components.AffilNeutral
	}
	return components.AffilUnknown
}

// SquadSymbolSpec resolves the symbol a squad marker draws: a player
// assignment wins, otherwise the roster's composition picks the icon.
func SquadSymbolSpec(ctx MapRenderCtx, squad ecs.Entity,
	roster *components.CommandRoster) components.SymbolSpec {
	if ctx.SquadOverrideMap != nil {
		if ov := ctx.SquadOverrideMap.Get(squad); ov != nil {
			return ov.Spec
		}
	}
	return squadSpecFromComposition(ctx, squad, roster)
}

// specialistIcon maps a role onto the icon a squad built around it earns.
// ok=false means the role reads as ordinary infantry (rifleman, leader,
// MG, grenadier, AT — all still an infantry symbol).
func specialistIcon(k components.UnitRoleKind) (components.IconKind, bool) {
	switch k {
	case components.RoleSniper:
		return components.IconRecon, true
	case components.RoleRadioOperator:
		return components.IconHQ, true
	case components.RoleMedic, components.RoleEngineer, components.RoleDemoMan:
		return components.IconSupply, true
	}
	return components.IconInfantry, false
}

// squadSpecFromComposition reads the roster: vehicles outvote infantry, and a
// specialist needs half the squad before it overrides the infantry icon — one
// medic in a rifle squad must not turn the marker into a supply unit.
func squadSpecFromComposition(ctx MapRenderCtx, squad ecs.Entity,
	roster *components.CommandRoster) components.SymbolSpec {
	spec := components.SymbolSpec{
		Affiliation: components.AffilUnknown,
		Dimension:   components.DimInfantryClass,
		Icon:        components.IconInfantry,
	}
	haveAffil := false
	if ctx.FactionMap != nil {
		if f := ctx.FactionMap.Get(squad); f != nil {
			spec.Affiliation = AffiliationForFaction(f.ID)
			haveAffil = true
		}
	}
	if roster == nil {
		return spec
	}

	// Tallied by icon, not by role: medic / engineer / demo all read as
	// support, so a mixed support element still clears the majority bar.
	var iconCount [iconSlots]uint8
	live, vehicles := 0, 0
	for i := uint8(0); i < roster.Count; i++ {
		mem := roster.Members[i]
		if mem == (ecs.Entity{}) || ctx.World == nil || !ctx.World.Alive(mem) {
			continue
		}
		live++
		// Squads predating upsertFaction carry no Faction of their own.
		if !haveAffil && ctx.FactionMap != nil {
			if f := ctx.FactionMap.Get(mem); f != nil {
				spec.Affiliation = AffiliationForFaction(f.ID)
				haveAffil = true
			}
		}
		if ctx.VehicleMap != nil && ctx.VehicleMap.Has(mem) {
			vehicles++
			continue
		}
		if ctx.RoleMap != nil {
			if r := ctx.RoleMap.Get(mem); r != nil {
				if icon, ok := specialistIcon(r.Kind); ok && int(icon) < iconSlots {
					iconCount[icon]++
				}
			}
		}
	}
	if live == 0 {
		return spec
	}
	if vehicles*2 >= live {
		spec.Dimension = components.DimVehicleClass
		spec.Icon = components.IconArmor
		return spec
	}

	best, bestN := components.IconInfantry, uint8(0)
	for icon, n := range iconCount {
		if n > bestN {
			best, bestN = components.IconKind(icon), n
		}
	}
	if int(bestN)*2 >= live {
		spec.Icon = best
	}
	return spec
}
