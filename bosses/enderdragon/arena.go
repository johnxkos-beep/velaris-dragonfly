package enderdragon

import (
	"math"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Arena layout constants. Loosely modelled on the real fight (10 pillars in
// a ring, varying heights, ~40-45 block radius) — not exact vanilla
// coordinates, just close enough to feel right.
const (
	pillarCount       = 10
	pillarRingRadius  = 43.0
	pillarHalfWidth   = 2  // pillars are (2*pillarHalfWidth+1) blocks wide — 5x5 — matching vanilla's chunky look, not a 1x1 pole
	dragonOrbitRadius = 34.0
	dragonOrbitHeight = 40.0
	crystalCageHeight = 2  // iron bars ring placed this many blocks below the crystal
	podiumRadius      = 5  // the central bedrock landing disc the dragon perches/dies on
	podiumTowerHeight = 3  // the 1-wide bedrock tower in the middle of the disc, matching vanilla's exit-portal pillar
	maxGroundSearch   = 96 // how far down to dig looking for solid ground before giving up
)

// pillarHeights gives each of the 10 pillars a different height (blocks of
// obsidian above the arena floor) so the fight has vertical variety, same
// spirit as vanilla's uneven pillar heights.
var pillarHeights = [pillarCount]int{8, 16, 11, 20, 13, 24, 9, 18, 12, 22}

// BuildArena places 10 obsidian pillars (each capped with a single bedrock
// block, an iron bars cage, and an End Crystal) in a ring around centre, a
// bedrock disc + 3-block centre tower (the thing the dragon periodically
// lands on, and where her egg ends up after death — see finishDeath in
// entity.go), then spawns the Ender Dragon orbiting above it all. centre's
// Y is treated as the arena floor level — run this standing on the End
// island's surface.
//
// Returns the spawned dragon so callers (e.g. the /spawnenderdragon
// command) can report back to the player, or nil if spawning failed.
func BuildArena(tx *world.Tx, centre mgl64.Vec3) *EnderDragon {
	base := cube.PosFromVec3(centre)
	counter := newCrystalCounter(pillarCount)

	buildPodium(tx, base)
	// Top surface of the disc is at base.Y+1; the tower adds
	// podiumTowerHeight more blocks on top of that, so its top surface —
	// where the dragon perches and the egg ends up — is at this Y. Built
	// from base.X()/base.Z() (the floored block coordinates), NOT
	// centre.X()/Z() directly — using the raw fractional player position
	// here was the cause of the egg landing off-by-one: it only matched
	// base.X()/Z() when the player happened to be standing in the first
	// half of that block.
	perchY := float64(base.Y() + 1 + podiumTowerHeight)
	perchPos := mgl64.Vec3{float64(base.X()) + 0.5, perchY, float64(base.Z()) + 0.5}

	for i := 0; i < pillarCount; i++ {
		angle := 2 * math.Pi * float64(i) / float64(pillarCount)
		px := base.X() + int(math.Round(pillarRingRadius*math.Cos(angle)))
		pz := base.Z() + int(math.Round(pillarRingRadius*math.Sin(angle)))
		height := pillarHeights[i]

		buildPillar(tx, cube.Pos{px, base.Y(), pz}, height)

		crystalPos := mgl64.Vec3{float64(px) + 0.5, float64(base.Y()+height+1), float64(pz) + 0.5}
		SpawnCrystal(tx, crystalPos, counter)
	}

	return Spawn(tx, centre, dragonOrbitRadius, dragonOrbitHeight, counter, perchPos)
}

// findGround scans straight down from (x, startY-1, z) for the first
// non-air block and returns the Y just above it (i.e. the ground surface).
// Falls back to startY (no adjustment) if nothing solid is found within
// maxGroundSearch blocks, rather than digging forever.
func findGround(tx *world.Tx, x, startY, z int) int {
	for y := startY - 1; y > startY-maxGroundSearch; y-- {
		if _, air := tx.Block(cube.Pos{x, y, z}).(block.Air); !air {
			return y + 1
		}
	}
	return startY
}

// buildPillar places a solid 5x5 obsidian column centred on (base.X,
// base.Z) — a real pillar, not a 1-block pole. It starts from the actual
// ground at that column (found via findGround, digging down if the island's
// terrain dips below base.Y — this is what was leaving pillars floating
// with a gap underneath) and runs up to base.Y+height. The top is capped
// with a single bedrock block dead centre (matching vanilla — just the one
// block the crystal floats above, not a full bedrock plate) with obsidian
// filling the rest of that top layer, plus a small iron-bars cage one block
// below the crystal.
//
// UNVERIFIED: block.IronBars{} wasn't confirmed against this Dragonfly
// version (unlike Obsidian/Bedrock, which are already used elsewhere in
// this repo — see worldgen/nether.go and commands/worlds.go). If `go build`
// complains about IronBars, delete the cage loop below — the pillar/cap/
// crystal still work fine without it, it's purely decorative.
func buildPillar(tx *world.Tx, base cube.Pos, height int) {
	groundY := findGround(tx, base.X(), base.Y(), base.Z())
	capY := base.Y() + height

	for y := groundY; y < capY; y++ {
		for dx := -pillarHalfWidth; dx <= pillarHalfWidth; dx++ {
			for dz := -pillarHalfWidth; dz <= pillarHalfWidth; dz++ {
				tx.SetBlock(cube.Pos{base.X() + dx, y, base.Z() + dz}, block.Obsidian{}, nil)
			}
		}
	}

	for dx := -pillarHalfWidth; dx <= pillarHalfWidth; dx++ {
		for dz := -pillarHalfWidth; dz <= pillarHalfWidth; dz++ {
			if dx == 0 && dz == 0 {
				continue // centre block is bedrock instead, placed below
			}
			tx.SetBlock(cube.Pos{base.X() + dx, capY, base.Z() + dz}, block.Obsidian{}, nil)
		}
	}
	tx.SetBlock(cube.Pos{base.X(), capY, base.Z()}, block.Bedrock{}, nil)

	cageY := capY + crystalCageHeight - 1
	for _, off := range []cube.Pos{{1, 0, 0}, {-1, 0, 0}, {0, 0, 1}, {0, 0, -1}} {
		tx.SetBlock(cube.Pos{base.X() + off.X(), cageY, base.Z() + off.Z()}, block.IronBars{}, nil)
	}
}

// buildPodium places a flat circular bedrock disc at the arena centre, plus
// a 1-wide, podiumTowerHeight-tall bedrock tower standing on top of it dead
// centre — the "circle with a tower in the middle" you get in vanilla,
// where the dragon periodically lands (see entity.go's perch phase) and
// where the Dragon Egg ends up on top of after death. Not a functioning
// exit portal (deferred, per earlier request) — no end_portal blocks, just
// the bedrock structure itself.
func buildPodium(tx *world.Tx, base cube.Pos) {
	r2 := podiumRadius * podiumRadius
	for dx := -podiumRadius; dx <= podiumRadius; dx++ {
		for dz := -podiumRadius; dz <= podiumRadius; dz++ {
			if dx*dx+dz*dz > r2 {
				continue
			}
			tx.SetBlock(cube.Pos{base.X() + dx, base.Y(), base.Z() + dz}, block.Bedrock{}, nil)
		}
	}
	for y := 1; y <= podiumTowerHeight; y++ {
		tx.SetBlock(cube.Pos{base.X(), base.Y() + y, base.Z()}, block.Bedrock{}, nil)
	}
}
