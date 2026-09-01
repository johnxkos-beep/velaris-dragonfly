// Package worldgen provides world.Generator implementations used by
// velaris-dragonfly. Dragonfly itself ships no real terrain generator — if
// UserConfig.World.Generator is left nil, every dimension (Overworld, Nether,
// End) falls back to a flat world. Nether is the one that mattered here, so
// Nether gets real (if not vanilla-accurate) terrain: netherrack mass carved
// into caverns by layered value noise, a lava sea at low elevations, bedrock
// caps, and scattered soul sand / glowstone / nether quartz ore.
//
// This is NOT a port of vanilla Nether generation (no vanilla noise router,
// no biome-specific fortresses/bastions/structures). It's a lightweight,
// dependency-free stand-in so the Nether stops being a flat plane. Swap it
// out later if a proper vanilla-parity generator becomes available.
package worldgen

import (
	"math"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

// netherLavaLevel is the height at/under which carved-out (non-solid) space
// is filled with lava instead of air, mimicking the vanilla Nether ocean.
const netherLavaLevel = 32

// Nether is a value-noise-based Nether terrain generator. Construct with
// NewNether.
type Nether struct {
	seed int64
}

// NewNether creates a Nether generator seeded with seed. Using the same seed
// on the same world will always carve the same caverns.
func NewNether(seed int64) Nether {
	return Nether{seed: seed}
}

// GenerateChunk implements world.Generator.
func (n Nether) GenerateChunk(pos world.ChunkPos, c *chunk.Chunk) {
	r := c.Range()
	min, max := int16(r.Min()), int16(r.Max())

	netherrackID := world.BlockRuntimeID(block.Netherrack{})
	bedrockID := world.BlockRuntimeID(block.Bedrock{})
	lavaID := world.BlockRuntimeID(block.Lava{Depth: 8, Still: true})
	soulSandID := world.BlockRuntimeID(block.SoulSand{})
	glowstoneID := world.BlockRuntimeID(block.Glowstone{})
	quartzOreID := world.BlockRuntimeID(block.NetherQuartzOre{})

	var netherBiome uint32
	if b, ok := world.BiomeByName("nether_wastes"); ok {
		netherBiome = uint32(b.EncodeBiome())
	}

	baseX, baseZ := int(pos.X())*16, int(pos.Z())*16

	for lx := uint8(0); lx < 16; lx++ {
		for lz := uint8(0); lz < 16; lz++ {
			wx, wz := baseX+int(lx), baseZ+int(lz)

			for y := min; y < max; y++ {
				c.SetBiome(lx, y, lz, netherBiome)

				// Bedrock floor and ceiling cap the dimension.
				if y == min || y == max-1 {
					c.SetBlock(lx, y, lz, 0, bedrockID)
					continue
				}

				if !n.solid(wx, int(y), wz) {
					if y <= netherLavaLevel {
						c.SetBlock(lx, y, lz, 0, lavaID)
					}
					// else: leave as air (zero value).
					continue
				}

				aboveOpen := !n.solid(wx, int(y)+1, wz)
				belowOpen := !n.solid(wx, int(y)-1, wz)

				blockID := netherrackID
				switch {
				case aboveOpen && n.hash(wx, int(y), wz, 11) < 0.03:
					// Exposed floor (open cave space above): occasional soul sand.
					blockID = soulSandID
				case belowOpen && n.hash(wx, int(y), wz, 23) < 0.02:
					// Exposed ceiling (open cave space below): occasional glowstone.
					blockID = glowstoneID
				case !aboveOpen && !belowOpen && n.hash(wx, int(y), wz, 37) < 0.012:
					// Fully buried: occasional nether quartz ore.
					blockID = quartzOreID
				}
				c.SetBlock(lx, y, lz, 0, blockID)
			}
		}
	}
}

// DefaultSpawn implements world.Generator. It returns a fixed point near the
// carved-out middle band of the Nether, which n.solid keeps mostly open —
// not a guaranteed-safe pocket, but a reasonable default away from the
// bedrock caps.
func (n Nether) DefaultSpawn() cube.Pos {
	return cube.Pos{0, 64, 0}
}

// solid reports whether the block at the world position passed should be
// solid netherrack mass (true) or carved-out open space (false).
func (n Nether) solid(x, y, z int) bool {
	d := 0.6*n.valueNoise3(float64(x)/24, float64(y)/16, float64(z)/24, 1) +
		0.4*n.valueNoise3(float64(x)/10, float64(y)/8, float64(z)/10, 2)
	return d > 0.42
}

// hash returns a deterministic pseudo-random value in [0, 1) for the given
// integer lattice point, seed and salt.
func (n Nether) hash(x, y, z int, salt int64) float64 {
	h := uint64(x)*374761393 + uint64(y)*668265263 + uint64(z)*2246822519 +
		uint64(n.seed)*3266489917 + uint64(salt)*198491317
	h = (h ^ (h >> 13)) * 1274126177
	h ^= h >> 16
	return float64(h&0xFFFFFF) / float64(0xFFFFFF)
}

// valueNoise3 samples trilinearly-interpolated value noise at a fractional
// 3D coordinate.
func (n Nether) valueNoise3(x, y, z float64, salt int64) float64 {
	x0, y0, z0 := math.Floor(x), math.Floor(y), math.Floor(z)
	xi, yi, zi := int(x0), int(y0), int(z0)
	fx, fy, fz := smooth(x-x0), smooth(y-y0), smooth(z-z0)

	c000 := n.hash(xi, yi, zi, salt)
	c100 := n.hash(xi+1, yi, zi, salt)
	c010 := n.hash(xi, yi+1, zi, salt)
	c110 := n.hash(xi+1, yi+1, zi, salt)
	c001 := n.hash(xi, yi, zi+1, salt)
	c101 := n.hash(xi+1, yi, zi+1, salt)
	c011 := n.hash(xi, yi+1, zi+1, salt)
	c111 := n.hash(xi+1, yi+1, zi+1, salt)

	x00 := lerp(c000, c100, fx)
	x10 := lerp(c010, c110, fx)
	x01 := lerp(c001, c101, fx)
	x11 := lerp(c011, c111, fx)

	y0v := lerp(x00, x10, fy)
	y1v := lerp(x01, x11, fy)

	return lerp(y0v, y1v, fz)
}

func smooth(t float64) float64 { return t * t * (3 - 2*t) }
func lerp(a, b, t float64) float64 { return a + (b-a)*t }

// Flat is a minimal flat generator, used here for Overworld/End so that
// setting UserConfig.World.Generator (required to give Nether real terrain)
// doesn't change how Overworld/End generate brand-new edge chunks. It mirrors
// Dragonfly's own flat-world fallback: a thin solid layer over bedrock.
type Flat struct {
	layers  []uint32
	bedrock uint32
	biome   uint32
	spawnY  int
}

// NewFlat creates a Flat generator for the given dimension. layers lists the
// block runtime IDs placed from the surface down (index 0 = topmost);
// everything below that down to (but not including) the bottom-most bedrock
// layer is filled with the last entry in layers.
func NewFlat(dim world.Dimension, biome world.Biome, layers []world.Block) Flat {
	f := Flat{layers: make([]uint32, len(layers))}
	if biome != nil {
		f.biome = uint32(biome.EncodeBiome())
	}
	for i, b := range layers {
		f.layers[i] = world.BlockRuntimeID(b)
	}
	f.bedrock = world.BlockRuntimeID(block.Bedrock{})
	f.spawnY = dim.Range().Min() + len(layers) + 1
	return f
}

// GenerateChunk implements world.Generator.
func (f Flat) GenerateChunk(_ world.ChunkPos, c *chunk.Chunk) {
	r := c.Range()
	min, max := int16(r.Min()), int16(r.Max())
	n := int16(len(f.layers))

	for x := uint8(0); x < 16; x++ {
		for z := uint8(0); z < 16; z++ {
			for y := min; y < max; y++ {
				c.SetBiome(x, y, z, f.biome)
				switch {
				case y == min:
					c.SetBlock(x, y, z, 0, f.bedrock)
				case y-min <= n:
					c.SetBlock(x, y, z, 0, f.layers[n-(y-min)])
				}
			}
		}
	}
}

// DefaultSpawn implements world.Generator.
func (f Flat) DefaultSpawn() cube.Pos {
	return cube.Pos{0, f.spawnY, 0}
}
