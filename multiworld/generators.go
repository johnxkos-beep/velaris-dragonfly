// Package multiworld ports czechpmdevs/MultiWorld's core idea — managing
// several independent, named worlds and moving players between them — onto
// Dragonfly. It does NOT port MultiWorld's vanilla overworld/nether terrain
// generator (muqsit/vanillagenerator, hundreds of files of ported
// Bukkit/Glowstone noise code): your repo already has its own overworld and
// nether generators in worldgen/, and new "overworld"/"nether"-type worlds
// created through this package reuse those instead of duplicating them.
//
// What IS ported here are MultiWorld's three custom generator types (void,
// skyblock, and its ender/end island), plus world create/delete/list/
// info/rename/teleport.
package multiworld

import (
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

// ---------------------------------------------------------------------
// Void — completely empty aside from a small platform at spawn, matching
// czechpmdevs/multiworld's VoidGenerator.
// ---------------------------------------------------------------------

type Void struct{}

func NewVoid(seed int64) Void { return Void{} }

func (Void) DefaultSpawn(world.Dimension) cube.Pos { return cube.Pos{0, 65, 0} }

func (Void) GenerateChunk(pos world.ChunkPos, c *chunk.Chunk) {
	// Only the chunk at the origin gets the platform; every other chunk is
	// left completely empty (air), same as MultiWorld's void generator.
	if pos.X() != 0 || pos.Z() != 0 {
		return
	}
	airRID := world.BlockRuntimeID(block.Air{})
	glassRID := world.BlockRuntimeID(block.Glass{})
	for x := 0; x < 16; x++ {
		for z := 0; z < 16; z++ {
			for y := int16(0); y < 128; y++ {
				c.SetBlock(uint8(x), y, uint8(z), 0, airRID)
			}
			// A 16x16 glass platform at y=64, centred on the spawn chunk.
			c.SetBlock(uint8(x), 64, uint8(z), 0, glassRID)
		}
	}
}

// ---------------------------------------------------------------------
// SkyBlock — a single small floating island at spawn, matching
// czechpmdevs/multiworld's SkyBlockGenerator (island shape only; the
// starter chest full of items that PHP MultiWorld drops on top isn't
// included here — say the word and I'll add a chest with a starter kit
// spawned right after world creation).
// ---------------------------------------------------------------------

type SkyBlock struct{}

func NewSkyBlock(seed int64) SkyBlock { return SkyBlock{} }

func (SkyBlock) DefaultSpawn(world.Dimension) cube.Pos { return cube.Pos{0, 69, 0} }

func (SkyBlock) GenerateChunk(pos world.ChunkPos, c *chunk.Chunk) {
	if pos.X() != 0 || pos.Z() != 0 {
		return
	}
	airRID := world.BlockRuntimeID(block.Air{})
	grassRID := world.BlockRuntimeID(block.Grass{})
	dirtRID := world.BlockRuntimeID(block.Dirt{})
	// FIXED from your build log: block.Leaves in your pinned dragonfly
	// commit has no Wood field (only block.Log does) — so it's just
	// Leaves{}, which resolves to the zero-value (oak) leaves. Log kept
	// Wood since that half of the line compiled fine in your log.
	logRID := world.BlockRuntimeID(block.Log{Wood: block.OakWood()})
	leavesRID := world.BlockRuntimeID(block.Leaves{})

	for x := 0; x < 16; x++ {
		for z := 0; z < 16; z++ {
			for y := int16(0); y < 128; y++ {
				c.SetBlock(uint8(x), y, uint8(z), 0, airRID)
			}
		}
	}

	// A small 5x5 island: dirt with a grass cap, roughly centred in the
	// spawn chunk (local coords 5..9), sitting at y=64.
	inIsland := func(lx, lz int) bool {
		dx, dz := lx-7, lz-7
		return dx*dx+dz*dz <= 6 // roughly circular, radius ~2.4
	}
	for lx := 0; lx < 16; lx++ {
		for lz := 0; lz < 16; lz++ {
			if !inIsland(lx, lz) {
				continue
			}
			c.SetBlock(uint8(lx), 62, uint8(lz), 0, dirtRID)
			c.SetBlock(uint8(lx), 63, uint8(lz), 0, dirtRID)
			c.SetBlock(uint8(lx), 64, uint8(lz), 0, grassRID)
		}
	}

	// A single oak tree at the island's centre.
	if pos.X() == 0 && pos.Z() == 0 {
		cx, cz := 7, 7
		for y := int16(65); y < 69; y++ {
			c.SetBlock(uint8(cx), y, uint8(cz), 0, logRID)
		}
		for dy := int16(-2); dy <= 0; dy++ {
			for dx := -2; dx <= 2; dx++ {
				for dz := -2; dz <= 2; dz++ {
					if dx == 0 && dz == 0 && dy < 0 {
						continue
					}
					lx, lz, ly := cx+dx, cz+dz, 69+dy
					if lx < 0 || lx > 15 || lz < 0 || lz > 15 {
						continue
					}
					c.SetBlock(uint8(lx), ly, uint8(lz), 0, leavesRID)
				}
			}
		}
	}
}

// ---------------------------------------------------------------------
// EndIsland — an end-stone island generator, visually matching what
// MultiWorld's EnderGenerator produced. This is built as a regular
// world.Overworld-dimension world, NOT the real world.End dimension —
// Dragonfly only has one singleton End dimension per server, so a second
// "end-themed" world can't literally be that dimension. If you specifically
// need real End mechanics (dragon fight, exit portal) on a second world,
// that's a much bigger ask — this gives you an end-stone arena/build-space
// instead, which covers what most servers actually use custom end worlds
// for.
// ---------------------------------------------------------------------

type EndIsland struct{}

func NewEndIsland(seed int64) EndIsland { return EndIsland{} }

func (EndIsland) DefaultSpawn(world.Dimension) cube.Pos { return cube.Pos{0, 65, 0} }

func (EndIsland) GenerateChunk(pos world.ChunkPos, c *chunk.Chunk) {
	if pos.X() != 0 || pos.Z() != 0 {
		return
	}
	airRID := world.BlockRuntimeID(block.Air{})
	endStoneRID := world.BlockRuntimeID(block.EndStone{})

	for x := 0; x < 16; x++ {
		for z := 0; z < 16; z++ {
			for y := int16(0); y < 128; y++ {
				c.SetBlock(uint8(x), y, uint8(z), 0, airRID)
			}
		}
	}
	inIsland := func(lx, lz int) bool {
		dx, dz := lx-7, lz-7
		return dx*dx+dz*dz <= 36 // radius ~6, bigger than the skyblock island
	}
	for lx := 0; lx < 16; lx++ {
		for lz := 0; lz < 16; lz++ {
			if !inIsland(lx, lz) {
				continue
			}
			for y := int16(60); y <= 64; y++ {
				c.SetBlock(uint8(lx), y, uint8(lz), 0, endStoneRID)
			}
		}
	}
}

// ---------------------------------------------------------------------
// generatorFor resolves a WorldType to a world.Generator. overworld/nether
// reuse your existing worldgen package; the rest are the custom generators
// above.
// ---------------------------------------------------------------------

func generatorFor(t WorldType, seed int64, overworldGen, netherGen world.Generator) world.Generator {
	switch t {
	case TypeVoid:
		return NewVoid(seed)
	case TypeSkyBlock:
		return NewSkyBlock(seed)
	case TypeEnd:
		return NewEndIsland(seed)
	case TypeNether:
		return netherGen
	default: // TypeOverworld
		return overworldGen
	}
}
