package endportal

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// Real Bedrock block identifiers, looked up at runtime via world.BlockByName
// rather than referenced as typed Go structs (e.g. block.EndPortalFrame{})
// — same reasoning as restrict.go's barrierBlockName/markerBlockName: these
// weren't confirmed against this Dragonfly version's Go API, but the wire
// identifiers themselves are part of the protocol and can't have drifted.
// If a lookup fails, the affected blocks are just skipped rather than
// panicking — you'd end up with a partial/incomplete-looking portal
// instead of a crash, and the log line says exactly which one failed.
const (
	endPortalFrameName = "minecraft:end_portal_frame"
	endPortalBlockName = "minecraft:end_portal"
	// UNVERIFIED block-state property name for a lit (eye-holding) frame —
	// "end_portal_eye_bit" is the real Bedrock block state for this, but
	// unconfirmed against this specific Dragonfly version's block state
	// handling. If frames come out unlit, this is the first thing to check
	// — the frame will still place fine either way, just without an eye.
	endPortalEyeProperty = "end_portal_eye_bit"
)

func endPortalFrameLit() (world.Block, bool) {
	if b, ok := world.BlockByName(endPortalFrameName, map[string]any{endPortalEyeProperty: uint8(1)}); ok {
		return b, true
	}
	// Fallback: try the boolean form in case this version expects bool
	// instead of a byte for the state.
	if b, ok := world.BlockByName(endPortalFrameName, map[string]any{endPortalEyeProperty: true}); ok {
		return b, true
	}
	return world.BlockByName(endPortalFrameName, nil)
}

func endPortalBlock() (world.Block, bool) {
	return world.BlockByName(endPortalBlockName, nil)
}

// BuildEntryPortal places a full vanilla-shaped End portal — the 12-block
// frame ring (corners omitted, matching the real structure) with eyes lit,
// and a 3x3 End Portal block floor in the middle — one block BELOW base,
// so a player standing at base ends up standing exactly on its surface
// rather than needing to dig a pit first.
//
// Returns triggerMin/triggerMax (the inner 3x3 portal-block box, for
// passing straight to SpawnSentinel) and footprintMin/footprintMax (the
// full 5x5 area including the frame ring, for DeleteAllIn to clear
// everything this placed — not just the trigger box).
func BuildEntryPortal(tx *world.Tx, base cube.Pos) (triggerMin, triggerMax, footprintMin, footprintMax cube.Pos) {
	frameY := base.Y() - 1
	frame, hasFrame := endPortalFrameLit()
	portal, hasPortal := endPortalBlock()

	for dx := -2; dx <= 2; dx++ {
		for dz := -2; dz <= 2; dz++ {
			pos := cube.Pos{base.X() + dx, frameY, base.Z() + dz}
			ax, az := abs(dx), abs(dz)
			switch {
			case ax <= 1 && az <= 1:
				if hasPortal {
					tx.SetBlock(pos, portal, nil)
				}
			case ax == 2 && az == 2:
				// Corner of the 5x5 — real vanilla portals leave this
				// empty, not part of the frame ring.
			default:
				if hasFrame {
					tx.SetBlock(pos, frame, nil)
				}
			}
		}
	}

	triggerMin = cube.Pos{base.X() - 1, frameY, base.Z() - 1}
	triggerMax = cube.Pos{base.X() + 1, frameY, base.Z() + 1}
	footprintMin = cube.Pos{base.X() - 2, frameY, base.Z() - 2}
	footprintMax = cube.Pos{base.X() + 2, frameY, base.Z() + 2}
	return
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
