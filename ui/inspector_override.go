package ui

import (
	"fmt"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/mlange-42/ark/ecs"

	"rts-go/components"
)

func drawOverrideBlock(ctx InspectorCtx, ent ecs.Entity, ov *components.TacticalOverride, x, y int32) int32 {
	label := components.ReasonLabel(ov.Reason)
	if label == "" {
		return y
	}
	drawText(ctx.Font, "Override:  "+label,
		x, y, inspectorFontSize, rl.Color{R: 230, G: 170, B: 90, A: 255})
	y += inspectorRowH

	reasonDetail := overrideReasonDetail(ctx, ent, ov)
	if reasonDetail != "" {
		drawText(ctx.Font, "Reason:    "+reasonDetail,
			x, y, inspectorFontSize, inspectorTextDim)
		y += inspectorRowH
	}
	if resume := components.ReasonResume(ov.Reason); resume != "" {
		drawText(ctx.Font, "Resume:    "+resume,
			x, y, inspectorFontSize, inspectorTextDim)
		y += inspectorRowH
	}
	return y
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
		threshold := overrideThreshold(ctx, ent)
		return fmt.Sprintf("Threat %.2f > threshold %.2f", total, threshold)
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
