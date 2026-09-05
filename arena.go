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
	dragonOrbitRadius = 34.0
	dragonOrbitHeight = 40.0
	crystalCageHeight = 2 // iron bars ring placed this many blocks below the crystal
)

// pillarHeights gives each of the 10 pillars a different height (blocks of
// obsidian above the arena floor) so the fight has vertical variety, same
// spirit as vanilla's uneven pillar heights.
var pillarHeights = [pillarCount]int{8, 16, 11, 20, 13, 24, 9, 18, 12, 22}

// BuildArena places 10 obsidian pillars (each capped with a bedrock plate,
// an iron bars cage, and an End Crystal) in a ring around centre, then
// spawns the Ender Dragon orbiting above them. centre's Y is treated as the
// arena floor level — run this standing on the End island's surface.
//
// Returns the spawned dragon so callers (e.g. the /spawnenderdragon
// command) can report back to the player, or nil if spawning failed.
func BuildArena(tx *world.Tx, centre mgl64.Vec3) *EnderDragon {
	base := cube.PosFromVec3(centre)
	counter := newCrystalCounter(pillarCount)

	for i := 0; i < pillarCount; i++ {
		angle := 2 * math.Pi * float64(i) / float64(pillarCount)
		px := base.X() + int(math.Round(pillarRingRadius*math.Cos(angle)))
		pz := base.Z() + int(math.Round(pillarRingRadius*math.Sin(angle)))
		height := pillarHeights[i]

		buildPillar(tx, cube.Pos{px, base.Y(), pz}, height)

		crystalPos := mgl64.Vec3{float64(px) + 0.5, float64(base.Y()+height+1), float64(pz) + 0.5}
		SpawnCrystal(tx, crystalPos, counter)
	}

	return Spawn(tx, centre, dragonOrbitRadius, dragonOrbitHeight, counter)
}

// buildPillar places a solid obsidian column at (base.X, base.Z) starting
// at base.Y, height blocks tall, capped with a single bedrock plate (so the
// crystal on top can't just be mined out from underneath) and a small
// iron-bars cage one block below where the crystal sits, matching vanilla's
// look.
//
// UNVERIFIED: block.IronBars{} wasn't confirmed against this Dragonfly
// version (unlike Obsidian/Bedrock, which are already used elsewhere in
// this repo — see worldgen/nether.go and commands/worlds.go). If `go build`
// complains about IronBars, delete the cage loop below — the pillar/cap/
// crystal still work fine without it, it's purely decorative.
func buildPillar(tx *world.Tx, base cube.Pos, height int) {
	for y := 0; y < height; y++ {
		tx.SetBlock(cube.Pos{base.X(), base.Y() + y, base.Z()}, block.Obsidian{}, nil)
	}
	capPos := cube.Pos{base.X(), base.Y() + height, base.Z()}
	tx.SetBlock(capPos, block.Bedrock{}, nil)

	cagePos := cube.Pos{base.X(), base.Y() + height + crystalCageHeight - 1, base.Z()}
	for _, off := range []cube.Pos{{1, 0, 0}, {-1, 0, 0}, {0, 0, 1}, {0, 0, -1}} {
		tx.SetBlock(cube.Pos{cagePos.X() + off.X(), cagePos.Y(), cagePos.Z() + off.Z()}, block.IronBars{}, nil)
	}
}
