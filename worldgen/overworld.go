// Package worldgen — this file adds Overworld, a non-flat "normal" terrain
// generator for DFWorlds destination worlds. Like Nether in nether.go, it's
// NOT vanilla-accurate (no real biome variety, no vanilla noise router,
// single grass/plains look everywhere) — it's a lightweight, dependency-free
// stand-in that gives rolling hills, a bit of underground cave space, and
// lake water at low points, so a "normal" world reads as actual terrain
// instead of the dead-flat Flat generator.
package worldgen

import (
	"math"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

const (
	overworldSeaLevel  = 62
	overworldBaseHeight = 68
	overworldAmplitude  = 22
)

// Overworld is a rolling-hills terrain generator. Construct with
// NewOverworld.
type Overworld struct {
	seed int64
}

// NewOverworld creates an Overworld generator seeded with seed. The same
// seed always produces the same terrain.
func NewOverworld(seed int64) Overworld {
	return Overworld{seed: seed}
}

// GenerateChunk implements world.Generator.
func (o Overworld) GenerateChunk(pos world.ChunkPos, c *chunk.Chunk) {
	r := c.Range()
	min, max := int16(r.Min()), int16(r.Max())

	grassID := world.BlockRuntimeID(block.Grass{})
	dirtID := world.BlockRuntimeID(block.Dirt{})
	stoneID := world.BlockRuntimeID(block.Stone{})
	bedrockID := world.BlockRuntimeID(block.Bedrock{})
	waterID := world.BlockRuntimeID(block.Water{Depth: 8, Still: true})

	var plainsBiome uint32
	if b, ok := world.BiomeByName("plains"); ok {
		plainsBiome = uint32(b.EncodeBiome())
	}

	baseX, baseZ := int(pos.X())*16, int(pos.Z())*16

	for lx := uint8(0); lx < 16; lx++ {
		for lz := uint8(0); lz < 16; lz++ {
			wx, wz := baseX+int(lx), baseZ+int(lz)
			height := o.heightAt(wx, wz)

			for y := min; y < max; y++ {
				c.SetBiome(lx, y, lz, plainsBiome)

				if y == min {
					c.SetBlock(lx, y, lz, 0, bedrockID)
					continue
				}

				switch {
				case int(y) > height:
					if int(y) <= overworldSeaLevel {
						c.SetBlock(lx, y, lz, 0, waterID)
					}
					// else: air.
				case o.cave(wx, int(y), wz) && int(y) < height-3:
					// Leave underground pockets open, but never within 3
					// blocks of the surface — keeps the exterior looking
					// like solid hills instead of swiss cheese.
				case int(y) == height:
					c.SetBlock(lx, y, lz, 0, grassID)
				case int(y) > height-4:
					c.SetBlock(lx, y, lz, 0, dirtID)
				default:
					c.SetBlock(lx, y, lz, 0, stoneID)
				}
			}
		}
	}
}

// DefaultSpawn implements world.Generator.
func (o Overworld) DefaultSpawn(world.Dimension) cube.Pos {
	h := o.heightAt(0, 0)
	return cube.Pos{0, h + 1, 0}
}

// heightAt returns the surface height (grass-block y) at a world column,
// using two octaves of value noise for gentle rolling hills.
func (o Overworld) heightAt(x, z int) int {
	n := 0.6*o.valueNoise2(float64(x)/64, float64(z)/64, 1) +
		0.4*o.valueNoise2(float64(x)/24, float64(z)/24, 2)
	return overworldBaseHeight + int((n-0.5)*2*overworldAmplitude)
}

// cave reports whether the given world position should be carved-out
// underground open space.
func (o Overworld) cave(x, y, z int) bool {
	d := o.valueNoise3(float64(x)/20, float64(y)/16, float64(z)/20, 3)
	return d > 0.72
}

// hash returns a deterministic pseudo-random value in [0, 1) for the given
// integer lattice point, seed and salt.
func (o Overworld) hash(x, y, z int, salt int64) float64 {
	h := uint64(x)*374761393 + uint64(y)*668265263 + uint64(z)*2246822519 +
		uint64(o.seed)*3266489917 + uint64(salt)*198491317
	h = (h ^ (h >> 13)) * 1274126177
	h ^= h >> 16
	return float64(h&0xFFFFFF) / float64(0xFFFFFF)
}

// valueNoise2 samples 2D value noise (y fixed at 0) at a fractional
// coordinate — used for the surface heightmap.
func (o Overworld) valueNoise2(x, z float64, salt int64) float64 {
	return o.valueNoise3(x, 0, z, salt)
}

// valueNoise3 samples trilinearly-interpolated value noise at a fractional
// 3D coordinate. Identical approach to Nether.valueNoise3 in nether.go.
func (o Overworld) valueNoise3(x, y, z float64, salt int64) float64 {
	x0, y0, z0 := math.Floor(x), math.Floor(y), math.Floor(z)
	xi, yi, zi := int(x0), int(y0), int(z0)
	fx, fy, fz := smooth(x-x0), smooth(y-y0), smooth(z-z0)

	c000 := o.hash(xi, yi, zi, salt)
	c100 := o.hash(xi+1, yi, zi, salt)
	c010 := o.hash(xi, yi+1, zi, salt)
	c110 := o.hash(xi+1, yi+1, zi, salt)
	c001 := o.hash(xi, yi, zi+1, salt)
	c101 := o.hash(xi+1, yi, zi+1, salt)
	c011 := o.hash(xi, yi+1, zi+1, salt)
	c111 := o.hash(xi+1, yi+1, zi+1, salt)

	x00 := lerp(c000, c100, fx)
	x10 := lerp(c010, c110, fx)
	x01 := lerp(c001, c101, fx)
	x11 := lerp(c011, c111, fx)

	y0v := lerp(x00, x10, fy)
	y1v := lerp(x01, x11, fy)

	return lerp(y0v, y1v, fz)
}
