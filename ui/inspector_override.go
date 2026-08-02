package ui

import (
	"fmt"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
)

func drawOverrideBlock(ctx InspectorCtx, col *Column, ent ecs.Entity, ov *components.TacticalOverride) {
	label := components.ReasonLabel(ov.Reason)
	if label == "" {
		return
	}
	TextRowClipped(col, &ctx.st, "Override:  "+label,
		rl.Color{R: 230, G: 170, B: 90, A: 255})

	if detail := overrideReasonDetail(ctx, ent, ov); detail != "" {
		TextRowClipped(col, &ctx.st, "Reason:    "+detail, ctx.st.TextDim)
	}
	if resume := components.ReasonResume(ov.Reason); resume != "" {
		TextRowClipped(col, &ctx.st, "Resume:    "+resume, ctx.st.TextDim)
	}
}

// overrideReasonDetail expands ReasonLabel with live numbers, e.g.
// "Threat 0.72 > threshold 0.45". Empty for placeholder reasons.
func overrideReasonDetail(ctx InspectorCtx, ent ecs.Entity, ov *components.TacticalOverride) string {
	switch ov.Reason {
	case components.TacticalOverrideUnderFire:
		total := float32(0)
		if ctx.ThreatMap != nil {
			if s := ctx.ThreatMap.Get(ent); s != nil {
				total = s.Total
			}
		}
		return fmt.Sprintf("Threat %.2f > threshold %.2f", total, overrideThreshold(ctx, ent))
	}
	return ""
}

func threatStateLabel(s components.ThreatState) string {
	switch s {
	case components.ThreatVigilant:
		return "Vigilant"
	case components.ThreatAlerted:
		return "Alerted"
	case components.ThreatThreatened:
		return "Threatened"
	}
	return "Safe"
}

// overrideThreshold mirrors SurvivalInstinct.thresholdFor (0.45 fallback
// for soloists).
func overrideThreshold(ctx InspectorCtx, ent ecs.Entity) float32 {
	if ctx.SquadMemberMap == nil || ctx.BehaviorRulesMap == nil {
		return 0.45
	}
	sm := ctx.SquadMemberMap.Get(ent)
	if sm == nil || sm.Squad == (ecs.Entity{}) {
		return 0.45
	}
	if br := ctx.BehaviorRulesMap.Get(sm.Squad); br != nil && br.SuppressionThreshold > 0 {
		return br.SuppressionThreshold
	}
	return 0.45
}
