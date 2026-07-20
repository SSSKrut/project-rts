package components

// Controller identifies who issues orders to an entity — the local human or
// an AI operator. Distinct from Faction (hostility group): co-op is two
// Controllers sharing one Faction. Explicit on every combatant and squad;
// selection and order input gate on Controller, never on Faction.
type Controller struct {
	Owner uint8
}

const (
	ControllerLocal uint8 = 0 // local human player
	ControllerAI    uint8 = 1 // operational AI (Phase 22-lite)
)
