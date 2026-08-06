// Package schematics ports the one piece of platz1de/EasyEdit that was asked
// for: loading a WorldEdit-style .schem file into memory (//load) and then
// pasting it on a separate command (//paste). It does not port EasyEdit's
// selection tools, brushes, patterns, or undo history — just the schematic
// file pipeline.
//
// Sponge Schematic files (.schem) are gzip-compressed, BIG ENDIAN Java NBT —
// this is the format WorldEdit/EasyEdit both read and write, and it's
// unrelated to Bedrock's own (little-endian) NBT that Dragonfly uses
// internally for chunks. We decode it with gophertunnel's nbt package (a
// dependency Dragonfly already pulls in) using nbt.BigEndian explicitly.
package schematics

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/sandertv/gophertunnel/minecraft/nbt"
)

// spongeV2 mirrors the Sponge Schematic Specification version 1/2 root
// compound — the format WorldEdit has written by default for years. Version
// 3 (which nests everything under a "Schematic"/"Blocks" sub-compound) is
// NOT handled here — Load will return a clear error if it sees one, rather
// than silently producing garbage. If your .schem files come out as v3
// (recent WorldEdit builds can be configured either way), tell me and I'll
// add a v3 branch.
type spongeV2 struct {
	Version       int32            `nbt:"Version"`
	DataVersion   int32            `nbt:"DataVersion"`
	Width         uint16           `nbt:"Width"`
	Height        uint16           `nbt:"Height"`
	Length        uint16           `nbt:"Length"`
	Offset        []int32          `nbt:"Offset"`
	PaletteMax    int32            `nbt:"PaletteMax"`
	Palette       map[string]int32 `nbt:"Palette"`
	BlockData     []byte           `nbt:"BlockData"`
	TileEntities  []map[string]any `nbt:"TileEntities"`  // v1 name
	BlockEntities []map[string]any `nbt:"BlockEntities"` // v2 name
}

// BlockState is a single parsed entry from the schematic's palette: the bare
// block name (e.g. "minecraft:oak_log") plus its Java block-state properties
// (e.g. {"axis": "y"}), still as raw strings — translation to a Bedrock
// world.Block happens in manager.go, not here, so this file has no
// dependency on Dragonfly's block package at all.
type BlockState struct {
	Name  string
	Props map[string]string
}

// Schematic is a fully-loaded, parsed .schem file sitting in memory,
// unrelated to any dragonfly world until Paste is called on it.
type Schematic struct {
	Width, Height, Length int
	// Offset is the schematic's stored paste offset (WorldEdit stores where
	// the original selection's minimum corner was relative to the point the
	// player was standing when they made the schematic — most callers can
	// ignore this and just paste at the corner, but it's kept for parity).
	Offset [3]int32
	// Palette maps a palette index (as used in Blocks) to its block state.
	Palette map[int32]BlockState
	// Blocks is Width*Height*Length long, indexed as
	// idx = y*Width*Length + z*Width + x (Sponge's iteration order), holding
	// a palette index per position.
	Blocks []int32
}

// LoadFile reads and parses a .schem file from disk.
//
// FIXED from your build log: this was named Load before, which collided
// with the `type Load struct` command in commands.go (same package, same
// name) — that's what caused both the "Load redeclared" error here and the
// "cannot convert path to type Load" error in manager.go (the compiler
// picked the command type instead of this function for the call in
// manager.go). Renamed to LoadFile; manager.go's call site is updated to
// match.
func LoadFile(path string) (*Schematic, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open schematic: %w", err)
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("schematic is not gzip data (corrupt file?): %w", err)
	}
	defer gz.Close()

	raw, err := io.ReadAll(gz)
	if err != nil {
		return nil, fmt.Errorf("decompress schematic: %w", err)
	}

	var root spongeV2
	dec := nbt.NewDecoderWithEncoding(bytes.NewReader(raw), nbt.BigEndian)
	if err := dec.Decode(&root); err != nil {
		return nil, fmt.Errorf("decode schematic NBT: %w", err)
	}

	if root.Palette == nil || len(root.Palette) == 0 {
		return nil, fmt.Errorf("no Palette/BlockData found — this is likely a v3 Sponge Schematic (nested under \"Schematic\"), which isn't supported yet")
	}

	blockEntities := root.BlockEntities
	if blockEntities == nil {
		blockEntities = root.TileEntities
	}
	_ = blockEntities // tile/block entity data (chests, signs, etc.) is parsed but
	// intentionally not applied on paste yet — block *shapes* paste fine,
	// container contents and sign text don't. Ask if you want that added.

	total := int(root.Width) * int(root.Height) * int(root.Length)
	blocks, err := decodeBlockData(root.BlockData, total)
	if err != nil {
		return nil, fmt.Errorf("decode block data: %w", err)
	}

	palette := make(map[int32]BlockState, len(root.Palette))
	for stateStr, idx := range root.Palette {
		palette[idx] = parseBlockState(stateStr)
	}

	s := &Schematic{
		Width:   int(root.Width),
		Height:  int(root.Height),
		Length:  int(root.Length),
		Palette: palette,
		Blocks:  blocks,
	}
	if len(root.Offset) == 3 {
		s.Offset = [3]int32{root.Offset[0], root.Offset[1], root.Offset[2]}
	}
	return s, nil
}

// decodeBlockData unpacks Sponge's varint-encoded (unsigned LEB128) palette
// index array. This part of the spec is simple and version-independent, so
// it's implemented directly rather than pulled from a library.
func decodeBlockData(data []byte, expected int) ([]int32, error) {
	out := make([]int32, 0, expected)
	var value, shift uint32
	for _, b := range data {
		value |= uint32(b&0x7F) << shift
		if b&0x80 == 0 {
			out = append(out, int32(value))
			value, shift = 0, 0
			continue
		}
		shift += 7
		if shift > 35 {
			return nil, fmt.Errorf("malformed varint in BlockData")
		}
	}
	if len(out) != expected {
		return nil, fmt.Errorf("BlockData has %d entries, expected %d (width*height*length)", len(out), expected)
	}
	return out, nil
}

// parseBlockState splits a Java block-state string like
// "minecraft:oak_log[axis=y]" into its base name and property map. A state
// with no brackets (e.g. "minecraft:stone") just gets an empty Props map.
func parseBlockState(s string) BlockState {
	name, rest, hasProps := strings.Cut(s, "[")
	if !hasProps {
		return BlockState{Name: name}
	}
	rest = strings.TrimSuffix(rest, "]")
	props := map[string]string{}
	if rest != "" {
		for _, pair := range strings.Split(rest, ",") {
			k, v, ok := strings.Cut(pair, "=")
			if ok {
				props[k] = v
			}
		}
	}
	return BlockState{Name: name, Props: props}
}

// At retrieves the palette entry at local coordinates (lx, ly, lz), where
// each axis is in [0, Width/Height/Length). ok is false if the coordinates
// are out of range.
func (s *Schematic) At(lx, ly, lz int) (BlockState, bool) {
	if lx < 0 || ly < 0 || lz < 0 || lx >= s.Width || ly >= s.Height || lz >= s.Length {
		return BlockState{}, false
	}
	idx := ly*s.Width*s.Length + lz*s.Width + lx
	pidx := s.Blocks[idx]
	bs, ok := s.Palette[pidx]
	return bs, ok
}

// propInt is a small helper future block-translation code can use to read a
// numeric Java property (e.g. "age" on crops) without hand-rolling
// strconv.Atoi at every call site.
func propInt(props map[string]string, key string) (int, bool) {
	v, ok := props[key]
	if !ok {
		return 0, false
	}
	n, err := strconv.Atoi(v)
	return n, err == nil
}
