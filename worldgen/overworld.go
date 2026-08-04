package worldgen

import (
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

const (
	overworldMinY, overworldMaxY = -64, 320
	seaLevel                     = 62
)

// Overworld is a basic biome-aware terrain generator for the overworld
// dimension. It is not vanilla-accurate, but produces varied hills, plains,
// forests, deserts and oceans using layered noise.
type Overworld struct{ seed int64 }

// NewOverworld creates an Overworld generator using the seed passed.
func NewOverworld(seed int64) Overworld { return Overworld{seed: seed} }

// DefaultSpawn implements world.Generator. It returns a spawn position sat
// just above the terrain height generated at the world origin (0, 0), using
// the same noise calculation as GenerateChunk uses for that column.
func (o Overworld) DefaultSpawn(world.Dimension) cube.Pos {
	h := fractal2D(0, 0, o.seed, 4, 0.5)
	height := 60 + int(h*40)
	return cube.Pos{0, height + 1, 0}
}

// GenerateChunk implements world.Generator.
func (o Overworld) GenerateChunk(pos world.ChunkPos, c *chunk.Chunk) {
	baseX, baseZ := int(pos.X())*16, int(pos.Z())*16

	airRID := world.BlockRuntimeID(block.Air{})
	stoneRID := world.BlockRuntimeID(block.Stone{})
	dirtRID := world.BlockRuntimeID(block.Dirt{})
	grassRID := world.BlockRuntimeID(block.Grass{})
	sandRID := world.BlockRuntimeID(block.Sand{})
	bedrockRID := world.BlockRuntimeID(block.Bedrock{})
	waterRID := world.BlockRuntimeID(block.Water{Depth: 8, Still: true})

	plains := findBiome("plains")
	forest := findBiome("forest")
	desert := findBiome("desert")
	hills := findBiome("windswept_hills", "extreme_hills", "mountains")
	ocean := findBiome("ocean")

	for x := uint8(0); x < 16; x++ {
		for z := uint8(0); z < 16; z++ {
			wx, wz := float64(baseX+int(x)), float64(baseZ+int(z))

			h := fractal2D(wx*0.01, wz*0.01, o.seed, 4, 0.5)
			temp := fractal2D(wx*0.004, wz*0.004, o.seed+1000, 3, 0.5)
			humid := fractal2D(wx*0.004, wz*0.004, o.seed+2000, 3, 0.5)
			height := int16(60 + int(h*40))

			var biome world.Biome
			var surfaceRID uint32
			switch {
			case height < seaLevel:
				biome, surfaceRID = ocean, sandRID
			case height > 100:
				biome, surfaceRID = hills, stoneRID
			case temp > 0.65 && humid < 0.35:
				biome, surfaceRID = desert, sandRID
			case humid > 0.6:
				biome, surfaceRID = forest, grassRID
			default:
				biome, surfaceRID = plains, grassRID
			}
			biomeID := uint32(biome.EncodeBiome())

			for y := overworldMinY; y < overworldMaxY; y++ {
				yy := int16(y)
				switch {
				case y == overworldMinY:
					c.SetBlock(x, yy, z, 0, bedrockRID)
				case yy < height-4:
					c.SetBlock(x, yy, z, 0, stoneRID)
				case yy < height:
					c.SetBlock(x, yy, z, 0, dirtRID)
				case yy == height:
					c.SetBlock(x, yy, z, 0, surfaceRID)
				case yy > height && yy <= seaLevel:
					c.SetBlock(x, yy, z, 0, waterRID)
				default:
					c.SetBlock(x, yy, z, 0, airRID)
				}
				c.SetBiome(x, yy, z, biomeID)
			}
		}
	}
}
