// This file adds End dimension terrain to the worldgen package (see
// nether.go's package doc for why this package exists at all — Dragonfly
// ships no real terrain generator of its own).
//
// End is the main End island ONLY, per request — no outer chorus islands,
// no End cities/gateways. It's a single roughly-circular end-stone island
// floating over the void, edge jittered with noise so it doesn't read as a
// perfect circle, with a little surface undulation so it isn't dead flat.
// The 10 obsidian pillars + End Crystals for the dragon fight are NOT part
// of this generator — those are built on demand by
// bosses/enderdragon.BuildArena when /spawnenderdragon runs, the same way
// the island itself doesn't need to already have them for the world to be
// valid (matches how the vanilla island technically still exists before a
// fight is started).
package worldgen

import (
	"math"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

// endIslandBaseY is the approximate surface height of the island, chosen to
// sit comfortably away from both the bottom-of-dimension bedrock and the
// pillar heights bosses/enderdragon.BuildArena builds on top of it.
const endIslandBaseY = 64

// End is a value-noise-based single-island End terrain generator. Construct
// with NewEnd.
type End struct {
	seed int64
}

// NewEnd creates an End generator seeded with seed. The same seed always
// carves the same island shape.
func NewEnd(seed int64) End {
	return End{seed: seed}
}

// GenerateChunk implements world.Generator.
func (e End) GenerateChunk(pos world.ChunkPos, c *chunk.Chunk) {
	r := c.Range()
	min, max := int16(r.Min()), int16(r.Max())

	endStoneID := world.BlockRuntimeID(block.EndStone{})

	var endBiome uint32
	if b, ok := world.BiomeByName("the_end"); ok {
		endBiome = uint32(b.EncodeBiome())
	}

	baseX, baseZ := int(pos.X())*16, int(pos.Z())*16

	for lx := uint8(0); lx < 16; lx++ {
		for lz := uint8(0); lz < 16; lz++ {
			wx, wz := baseX+int(lx), baseZ+int(lz)
			top := e.islandTop(wx, wz)

			for y := min; y < max; y++ {
				c.SetBiome(lx, y, lz, endBiome)

				// No bedrock floor — the real End has none; it's genuine
				// void below the islands (fall forever / take void damage,
				// same as vanilla). An earlier version capped the bottom
				// with bedrock "so nothing falls forever", which was wrong
				// — that's not how the actual dimension behaves, and it's
				// what was reported as bedrock where there should be void.
				if top > 0 && int(y) <= top {
					c.SetBlock(lx, y, lz, 0, endStoneID)
				}
			}
		}
	}
}

// DefaultSpawn implements world.Generator. Lands just above the island
// surface near its centre.
func (e End) DefaultSpawn(world.Dimension) cube.Pos {
	return cube.Pos{0, endIslandBaseY + 6, 0}
}

// islandTop returns the topmost solid Y at world column (x, z), or 0 if
// that column falls outside the island entirely (void).
func (e End) islandTop(x, z int) int {
	dist := math.Hypot(float64(x), float64(z))

	// Edge radius jitters between ~60 and ~90 blocks from centre so the
	// coastline reads as organic instead of a perfect circle — sampled at
	// a large noise scale so the jitter forms broad bays/points rather than
	// jagged noise.
	edge := e.valueNoise3(float64(x)/56, 0, float64(z)/56, 5)
	radius := 60.0 + edge*30.0
	if dist >= radius {
		return 0
	}

	// Gentle dome: highest near the centre, tapering down toward the
	// coastline, plus a smaller-scale bump layer so the surface isn't
	// perfectly smooth.
	t := 1 - dist/radius
	bump := e.valueNoise3(float64(x)/22, 0, float64(z)/22, 9)
	return int(math.Round(endIslandBaseY + t*10 + bump*6 - 3))
}

// hash returns a deterministic pseudo-random value in [0, 1) for the given
// integer lattice point, seed and salt. Same construction as Nether.hash —
// duplicated here (rather than shared) so End has no dependency on Nether's
// internals.
func (e End) hash(x, y, z int, salt int64) float64 {
	h := uint64(x)*374761393 + uint64(y)*668265263 + uint64(z)*2246822519 +
		uint64(e.seed)*3266489917 + uint64(salt)*198491317
	h = (h ^ (h >> 13)) * 1274126177
	h ^= h >> 16
	return float64(h&0xFFFFFF) / float64(0xFFFFFF)
}

// valueNoise3 samples trilinearly-interpolated value noise at a fractional
// 3D coordinate (y is passed as 0 throughout this file — the island shape
// only varies over x/z — but kept 3D to reuse the same construction as
// Nether.valueNoise3).
func (e End) valueNoise3(x, y, z float64, salt int64) float64 {
	x0, y0, z0 := math.Floor(x), math.Floor(y), math.Floor(z)
	xi, yi, zi := int(x0), int(y0), int(z0)
	fx, fy, fz := smooth(x-x0), smooth(y-y0), smooth(z-z0)

	c000 := e.hash(xi, yi, zi, salt)
	c100 := e.hash(xi+1, yi, zi, salt)
	c010 := e.hash(xi, yi+1, zi, salt)
	c110 := e.hash(xi+1, yi+1, zi, salt)
	c001 := e.hash(xi, yi, zi+1, salt)
	c101 := e.hash(xi+1, yi, zi+1, salt)
	c011 := e.hash(xi, yi+1, zi+1, salt)
	c111 := e.hash(xi+1, yi+1, zi+1, salt)

	x00 := lerp(c000, c100, fx)
	x10 := lerp(c010, c110, fx)
	x01 := lerp(c001, c101, fx)
	x11 := lerp(c011, c111, fx)

	y0v := lerp(x00, x10, fy)
	y1v := lerp(x01, x11, fy)

	return lerp(y0v, y1v, fz)
}
