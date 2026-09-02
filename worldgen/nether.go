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
				case aboveOpen && n.soulSandPatch(wx, wz):
					// Any exposed floor inside a soul sand patch is soul
					// sand — patches are large contiguous areas (like a
					// mini biome), not single scattered blocks.
					blockID = soulSandID
				case belowOpen && n.glowstoneCluster(wx, int(y), wz):
					// Blob-shaped cluster of glowstone hanging off the
					// ceiling, matching vanilla's clumped look.
					blockID = glowstoneID
				case !aboveOpen && !belowOpen && n.quartzVein(wx, int(y), wz):
					// Short worm-shaped vein through solid rock, not an
					// isolated single ore block.
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
func (n Nether) DefaultSpawn(world.Dimension) cube.Pos {
	return cube.Pos{0, 64, 0}
}

// solid reports whether the block at the world position passed should be
// solid netherrack mass (true) or carved-out open space (false). Uses two
// broad noise octaves (large feature size, so caverns read as roomy
// passages instead of noisy static) plus a vertical bias that opens up big
// space through the middle of the dimension and tapers back to solid near
// the bedrock floor/ceiling caps — that's what keeps the roof-to-floor gap
// from feeling cramped.
func (n Nether) solid(x, y, z int) bool {
	d := 0.55*n.valueNoise3(float64(x)/42, float64(y)/30, float64(z)/42, 1) +
		0.45*n.valueNoise3(float64(x)/18, float64(y)/14, float64(z)/18, 2)

	const mid = 64.0
	d -= 0.16 * (1 - math.Min(1, math.Abs(float64(y)-mid)/48))

	return d > 0.44
}

// soulSandPatch reports whether the (x, z) column falls inside a soul sand
// patch. Uses one big-radius 2D noise field (y is ignored) so patches are
// large, contiguous, biome-like areas instead of single random floor
// blocks — any floor within a patch is soul sand, any floor outside it
// isn't.
func (n Nether) soulSandPatch(x, z int) bool {
	v := n.valueNoise3(float64(x)/48, 0, float64(z)/48, 71)
	return v > 0.72
}

// glowstoneCluster reports whether the ceiling block at the given world
// position belongs to a glowstone cluster. Space is divided into 16-block
// macro cells; roughly 30% of cells get one jittered cluster centre, and
// blocks within a shrinking radius of that centre (flattened vertically so
// clusters read as hanging blobs rather than spheres) become glowstone —
// matching vanilla's clumped-on-the-ceiling look instead of scattered
// single blocks.
func (n Nether) glowstoneCluster(x, y, z int) bool {
	const cell = 16
	cx, cy, cz := macroCell(x, cell), macroCell(y, cell), macroCell(z, cell)
	if n.hash(cx, cy, cz, 81) > 0.3 {
		return false
	}
	ccx := cx + int(n.hash(cx, cy, cz, 82)*cell) - cell/2
	ccy := cy + int(n.hash(cx, cy, cz, 83)*cell) - cell/2
	ccz := cz + int(n.hash(cx, cy, cz, 84)*cell) - cell/2
	dx, dy, dz := float64(x-ccx), float64(y-ccy), float64(z-ccz)
	dist := math.Sqrt(dx*dx + dy*dy*2.5 + dz*dz)
	radius := 1.6 + n.hash(cx, cy, cz, 85)*1.4
	return dist < radius
}

// quartzVein reports whether the given world position belongs to a nether
// quartz vein. Space is divided into 20-block macro cells; roughly 22% get
// a vein, walked as a short 8-step chain from a jittered seed point —
// producing a worm-shaped cluster of blocks (vanilla-style vein) instead of
// isolated single ore blocks.
func (n Nether) quartzVein(x, y, z int) bool {
	const cell = 20
	const steps = 8
	cx, cy, cz := macroCell(x, cell), macroCell(y, cell), macroCell(z, cell)
	if n.hash(cx, cy, cz, 91) > 0.22 {
		return false
	}
	px := float64(cx) + (n.hash(cx, cy, cz, 92)-0.5)*cell
	py := float64(cy) + (n.hash(cx, cy, cz, 93)-0.5)*cell
	pz := float64(cz) + (n.hash(cx, cy, cz, 94)-0.5)*cell
	for i := 0; i < steps; i++ {
		if math.Abs(px-float64(x)) < 1 && math.Abs(py-float64(y)) < 1 && math.Abs(pz-float64(z)) < 1 {
			return true
		}
		salt := int64(100 + i*3)
		px += n.hash(cx, cy, cz, salt)*2 - 1
		py += n.hash(cx, cy, cz, salt+1)*2 - 1
		pz += n.hash(cx, cy, cz, salt+2)*2 - 1
	}
	return false
}

// macroCell snaps v down to the centre of its enclosing cell-sized bucket.
func macroCell(v, cell int) int {
	return int(math.Floor(float64(v)/float64(cell)))*cell + cell/2
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
func (f Flat) DefaultSpawn(world.Dimension) cube.Pos {
	return cube.Pos{0, f.spawnY, 0}
}
