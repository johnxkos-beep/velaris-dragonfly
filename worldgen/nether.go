package worldgen

import (
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

const (
	netherMinY, netherMaxY = 0, 128
	lavaLevel              = 31
)

// Nether is a basic biome-aware cavern generator for the nether dimension.
// It carves open caverns out of solid netherrack using 3D noise, fills low
// open pockets with lava, and splits nether wastes / soul sand valley by 2D
// noise.
type Nether struct{ seed int64 }

// NewNether creates a Nether generator using the seed passed.
func NewNether(seed int64) Nether { return Nether{seed: seed + 5000} }

// DefaultSpawn implements world.Generator. It walks up from just above the
// lava level at the world origin (0, 0) until it finds the first open (non-
// solid) column position, using the same cave noise GenerateChunk uses for
// that column. If nothing open is found in range, it falls back to a fixed
// position just above the lava level.
func (n Nether) DefaultSpawn() cube.Pos {
	for y := lavaLevel + 1; y < netherMaxY-1; y++ {
		cave := fractal3D(0, float64(y)*0.05, 0, n.seed, 3, 0.5)
		if cave <= 0.55 {
			return cube.Pos{0, y, 0}
		}
	}
	return cube.Pos{0, lavaLevel + 1, 0}
}

// GenerateChunk implements world.Generator.
func (n Nether) GenerateChunk(pos world.ChunkPos, c *chunk.Chunk) {
	baseX, baseZ := int(pos.X())*16, int(pos.Z())*16

	airRID := world.BlockRuntimeID(block.Air{})
	netherrackRID := world.BlockRuntimeID(block.Netherrack{})
	soulSandRID := world.BlockRuntimeID(block.SoulSand{})
	bedrockRID := world.BlockRuntimeID(block.Bedrock{})
	lavaRID := world.BlockRuntimeID(block.Lava{Depth: 8, Still: true})

	wastes, _ := world.BiomeByName("minecraft:nether_wastes")
	soulValley, _ := world.BiomeByName("minecraft:soul_sand_valley")

	for x := uint8(0); x < 16; x++ {
		for z := uint8(0); z < 16; z++ {
			wx, wz := float64(baseX+int(x)), float64(baseZ+int(z))
			biomeNoise := fractal2D(wx*0.006, wz*0.006, n.seed, 3, 0.5)
			isSoulValley := biomeNoise > 0.6

			var biome world.Biome
			var surfaceRID uint32
			if isSoulValley {
				biome, surfaceRID = soulValley, soulSandRID
			} else {
				biome, surfaceRID = wastes, netherrackRID
			}
			biomeID := uint32(biome.EncodeBiome())

			for y := netherMinY; y < netherMaxY; y++ {
				yy := int16(y)
				c.SetBiome(x, yy, z, biomeID)

				if y == netherMinY || y == netherMaxY-1 {
					c.SetBlock(x, yy, z, 0, bedrockRID)
					continue
				}

				cave := fractal3D(wx*0.03, float64(y)*0.05, wz*0.03, n.seed, 3, 0.5)
				solid := cave > 0.55

				switch {
				case !solid && y <= lavaLevel:
					c.SetBlock(x, yy, z, 0, lavaRID)
				case !solid:
					c.SetBlock(x, yy, z, 0, airRID)
				case y <= netherMinY+3:
					c.SetBlock(x, yy, z, 0, surfaceRID)
				default:
					c.SetBlock(x, yy, z, 0, netherrackRID)
				}
			}
		}
	}
}
