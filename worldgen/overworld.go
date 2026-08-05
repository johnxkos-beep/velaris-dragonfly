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
	// deepslateTransition is how many blocks below y=0 the stone-to-deepslate
	// blend zone spans, matching vanilla's rough transition band. Below
	// -deepslateTransition it's pure deepslate; above y=0 it's pure stone.
	deepslateTransition = 8
)

// Overworld is a basic biome-aware terrain generator for the overworld
// dimension. It is not vanilla-accurate, but produces varied hills, plains,
// forests, taiga, dark forest, snowy tundra, deserts and oceans using
// layered noise, roughly in line with how vanilla picks biomes from
// temperature/humidity/height.
type Overworld struct{ seed int64 }

// NewOverworld creates an Overworld generator using the seed passed.
func NewOverworld(seed int64) Overworld { return Overworld{seed: seed} }

// terrainHeight computes the surface height at a world column by blending
// three noise scales: a low-frequency "continent" layer for broad
// land/ocean shape, a mid-frequency "hills" layer for rolling terrain, and a
// high-frequency "detail" layer to break up the too-smooth/circular look a
// single octave set produces. The result is centred so both oceans
// (height < seaLevel) and mountains (height > 100) actually occur.
func (o Overworld) terrainHeight(wx, wz float64) int {
	continent := fractal2D(wx*0.003, wz*0.003, o.seed, 3, 0.5)
	hills := fractal2D(wx*0.01, wz*0.01, o.seed+100, 4, 0.5)
	detail := fractal2D(wx*0.05, wz*0.05, o.seed+200, 2, 0.5)
	combined := continent*0.5 + hills*0.35 + detail*0.15
	return 40 + int((combined-0.5)*170)
}

// DefaultSpawn implements world.Generator. It returns a spawn position sat
// just above the terrain height generated at the world origin (0, 0), using
// the same noise calculation as GenerateChunk uses for that column.
func (o Overworld) DefaultSpawn(world.Dimension) cube.Pos {
	height := o.terrainHeight(0, 0)
	if height < seaLevel {
		height = seaLevel
	}
	return cube.Pos{0, height + 1, 0}
}

// GenerateChunk implements world.Generator.
func (o Overworld) GenerateChunk(pos world.ChunkPos, c *chunk.Chunk) {
	baseX, baseZ := int(pos.X())*16, int(pos.Z())*16

	airRID := world.BlockRuntimeID(block.Air{})
	stoneRID := world.BlockRuntimeID(block.Stone{})
	deepslateRID := world.BlockRuntimeID(block.Deepslate{})
	dirtRID := world.BlockRuntimeID(block.Dirt{})
	grassRID := world.BlockRuntimeID(block.Grass{})
	sandRID := world.BlockRuntimeID(block.Sand{})
	bedrockRID := world.BlockRuntimeID(block.Bedrock{})
	waterRID := world.BlockRuntimeID(block.Water{Depth: 8, Still: true})
	// block.SnowLayer{} doesn't exist in this Dragonfly version (confirmed
	// by build error) — Snow{Layers: 1} is the thin single-layer snow
	// covering, matching the pattern block.Water{Depth: ...} uses above.
	snowLayerRID := world.BlockRuntimeID(block.Snow{Layers: 1})

	plains := findBiome("plains")
	forest := findBiome("forest")
	taiga := findBiome("taiga")
	darkForest := findBiome("dark_forest", "roofed_forest")
	snowy := findBiome("snowy_plains", "snowy_tundra", "ice_plains", "frozen")
	desert := findBiome("desert")
	hillsBiome := findBiome("windswept_hills", "extreme_hills", "mountains")
	ocean := findBiome("ocean")

	for x := uint8(0); x < 16; x++ {
		for z := uint8(0); z < 16; z++ {
			wx, wz := float64(baseX+int(x)), float64(baseZ+int(z))

			height := int16(o.terrainHeight(wx, wz))
			temp := fractal2D(wx*0.0025, wz*0.0025, o.seed+1000, 3, 0.5)
			humid := fractal2D(wx*0.0025, wz*0.0025, o.seed+2000, 3, 0.5)

			var biome world.Biome
			var surfaceRID, subsurfaceRID uint32
			snowCap := false
			switch {
			case height < seaLevel:
				biome, surfaceRID, subsurfaceRID = ocean, sandRID, sandRID
			case height > 120:
				biome, surfaceRID, subsurfaceRID = hillsBiome, stoneRID, stoneRID
				snowCap = height > 135
			case height > 95:
				biome, surfaceRID, subsurfaceRID = hillsBiome, grassRID, dirtRID
			case temp < 0.25:
				biome, surfaceRID, subsurfaceRID = snowy, grassRID, dirtRID
				snowCap = true
			case temp < 0.4 && humid > 0.35:
				biome, surfaceRID, subsurfaceRID = taiga, grassRID, dirtRID
			case temp > 0.7 && humid < 0.3:
				biome, surfaceRID, subsurfaceRID = desert, sandRID, sandRID
			case humid > 0.65 && temp > 0.35 && temp < 0.65:
				biome, surfaceRID, subsurfaceRID = darkForest, grassRID, dirtRID
			case humid > 0.5:
				biome, surfaceRID, subsurfaceRID = forest, grassRID, dirtRID
			default:
				biome, surfaceRID, subsurfaceRID = plains, grassRID, dirtRID
			}
			biomeID := uint32(biome.EncodeBiome())

			for y := overworldMinY; y < overworldMaxY; y++ {
				yy := int16(y)
				switch {
				case y == overworldMinY:
					c.SetBlock(x, yy, z, 0, bedrockRID)
				case yy < height-4:
					// Below y=0 blend stone into deepslate over an 8 block
					// band, fully deepslate by y=-8, matching vanilla.
					rockRID := stoneRID
					if yy < 0 {
						if yy <= -deepslateTransition {
							rockRID = deepslateRID
						} else {
							blend := fractal3D(wx*0.15, float64(yy)*0.15, wz*0.15, o.seed+4000, 2, 0.5)
							threshold := float64(-yy) / float64(deepslateTransition)
							if blend < threshold {
								rockRID = deepslateRID
							}
						}
					}
					c.SetBlock(x, yy, z, 0, rockRID)
				case yy < height:
					c.SetBlock(x, yy, z, 0, subsurfaceRID)
				case yy == height:
					c.SetBlock(x, yy, z, 0, surfaceRID)
				case snowCap && yy == height+1:
					c.SetBlock(x, yy, z, 0, snowLayerRID)
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
