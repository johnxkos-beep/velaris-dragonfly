// Package schematic is a Go port of exactly two commands from the
// PocketMine-MP plugin EasyEdit: /save and /load (EasyEdit's own aliases
// for /saveschematic and /loadschematic). Nothing else from EasyEdit —
// no brushes, selections-by-click, history, generation commands, etc. —
// was ported. Per request, this is intentionally small.
//
// Format note: EasyEdit always *saves* as a Sponge .schem (Java Edition
// NBT layout, translated through PocketMine's own block-state upgrader)
// but can *load* several formats including Bedrock's .mcstructure.
// Reproducing either exactly would mean re-implementing PocketMine's NBT
// block-state upgrader in Go with no way to compile-check it in this
// session (no network access here to fetch NBT libraries or check field
// names against df-mc/dragonfly's real source). Instead this uses a
// small custom binary format — magic "RFSC", extension .rfschem — built
// entirely on world.BlockRuntimeID, which is already proven to compile
// and run elsewhere in this repo (see worldgen/nether.go). No new
// dependencies are needed; only the standard library.
//
// Trade-off: .rfschem files only work on THIS server (same block/version
// set as when they were saved). They are not compatible with WorldEdit
// .schem files or Bedrock .mcstructure files from other tools.
//
// Known limitations (documented rather than silently dropped):
//   - Block entities (chest contents, sign text, etc.) are NOT saved,
//     only the block itself. A saved chest loads back in empty.
//     EasyEdit's own .mcstructure reader has the same gap for entities
//     ("//TODO: entities" in its source); this port has it for tiles
//     too, to keep the format simple enough to get right without a
//     compiler available.
//   - Entities (mobs, item frames, etc.) inside the region are NOT
//     saved.
//
// UNVERIFIED: world.BlockByRuntimeID (used in Import, below) is not used
// anywhere else in this repo, so unlike BlockRuntimeID it hasn't been
// proven against your actual dragonfly v0.11.4 build. If `go build`
// fails specifically on that call, paste the compiler error back — it's
// a one-line fix (wrong return signature or package).
package schematic

import (
	"bufio"
	"compress/gzip"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

const (
	magic         = "RFSC"
	formatVersion = 1

	// Dir is where .rfschem files live, relative to the server process's
	// working directory (the same place the world/ folder sits, i.e. the
	// server's root in the Pterodactyl container). Upload files here over
	// SFTP or the Pterodactyl file manager to make them available to
	// /load; a fresh server creates this folder itself on first /save.
	Dir = "schematics"

	// maxVolume caps how many blocks a single /save or /load can touch,
	// so a mistyped huge region can't freeze the tick loop (every block
	// read/write happens inline on the world transaction). Raise it if
	// you need bigger builds and don't mind a longer pause while it runs.
	maxVolume = 262144 // 64 x 64 x 64
)

// Size is the block dimensions of a schematic.
type Size struct{ X, Y, Z int }

// Volume returns the total block count.
func (s Size) Volume() int { return s.X * s.Y * s.Z }

// path returns the on-disk path for a schematic name: it strips any
// directory components and a pre-existing .rfschem suffix, so names
// can't escape Dir via "../" and typing the extension twice is harmless.
func path(name string) string {
	name = filepath.Base(strings.TrimSuffix(name, ".rfschem"))
	return filepath.Join(Dir, name+".rfschem")
}

// List returns the names (without extension) of every .rfschem file
// currently in Dir, sorted alphabetically.
func List() []string {
	entries, err := os.ReadDir(Dir)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if n, ok := strings.CutSuffix(e.Name(), ".rfschem"); ok {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	return names
}

// Exists reports whether a schematic with this name is on disk.
func Exists(name string) bool {
	_, err := os.Stat(path(name))
	return err == nil
}

// Export reads every block in the inclusive box between c1 and c2 out of
// tx and writes them to Dir/name.rfschem, gzip-compressed.
func Export(tx *world.Tx, name string, c1, c2 cube.Pos) (Size, error) {
	min := cube.Pos{minInt(c1.X(), c2.X()), minInt(c1.Y(), c2.Y()), minInt(c1.Z(), c2.Z())}
	max := cube.Pos{maxInt(c1.X(), c2.X()), maxInt(c1.Y(), c2.Y()), maxInt(c1.Z(), c2.Z())}
	size := Size{max.X() - min.X() + 1, max.Y() - min.Y() + 1, max.Z() - min.Z() + 1}
	if size.Volume() > maxVolume {
		return size, fmt.Errorf("that region is %d blocks, over the %d limit — pick a smaller box", size.Volume(), maxVolume)
	}

	if err := os.MkdirAll(Dir, 0755); err != nil {
		return size, fmt.Errorf("couldn't create %s/ directory: %w", Dir, err)
	}
	f, err := os.Create(path(name))
	if err != nil {
		return size, fmt.Errorf("couldn't create schematic file: %w", err)
	}
	defer f.Close()

	gz := gzip.NewWriter(f)
	w := bufio.NewWriter(gz)

	if _, err := w.WriteString(magic); err != nil {
		return size, err
	}
	if err := w.WriteByte(formatVersion); err != nil {
		return size, err
	}
	writeInt32(w, int32(size.X))
	writeInt32(w, int32(size.Y))
	writeInt32(w, int32(size.Z))

	for x := min.X(); x <= max.X(); x++ {
		for y := min.Y(); y <= max.Y(); y++ {
			for z := min.Z(); z <= max.Z(); z++ {
				id := world.BlockRuntimeID(tx.Block(cube.Pos{x, y, z}))
				writeUint32(w, id)
			}
		}
	}

	if err := w.Flush(); err != nil {
		return size, fmt.Errorf("couldn't write schematic file: %w", err)
	}
	if err := gz.Close(); err != nil {
		return size, fmt.Errorf("couldn't finish compressing schematic file: %w", err)
	}
	return size, nil
}

// Import reads Dir/name.rfschem and pastes it into tx with its min
// corner at origin, fully overwriting whatever was there (including
// with air, wherever the saved region was empty), matching how a
// WorldEdit-style paste behaves.
func Import(tx *world.Tx, name string, origin cube.Pos) (Size, error) {
	f, err := os.Open(path(name))
	if err != nil {
		return Size{}, fmt.Errorf("couldn't open schematic file: %w", err)
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return Size{}, fmt.Errorf("couldn't read schematic file (corrupt or not a .rfschem): %w", err)
	}
	defer gz.Close()
	r := bufio.NewReader(gz)

	got := make([]byte, len(magic))
	if _, err := readFull(r, got); err != nil || string(got) != magic {
		return Size{}, errors.New("not a valid .rfschem file")
	}
	ver, err := r.ReadByte()
	if err != nil || ver != formatVersion {
		return Size{}, errors.New("unsupported .rfschem version")
	}

	sx, err1 := readInt32(r)
	sy, err2 := readInt32(r)
	sz, err3 := readInt32(r)
	if err1 != nil || err2 != nil || err3 != nil {
		return Size{}, errors.New("truncated schematic file")
	}
	size := Size{int(sx), int(sy), int(sz)}
	if size.X <= 0 || size.Y <= 0 || size.Z <= 0 || size.Volume() > maxVolume {
		return size, fmt.Errorf("schematic is %d blocks, over the %d limit", size.Volume(), maxVolume)
	}

	for x := 0; x < size.X; x++ {
		for y := 0; y < size.Y; y++ {
			for z := 0; z < size.Z; z++ {
				id, err := readUint32(r)
				if err != nil {
					return size, errors.New("truncated schematic file")
				}
				b, ok := world.BlockByRuntimeID(id)
				if !ok {
					// Block ID from a different server build/version —
					// skip rather than place something wrong or crash.
					continue
				}
				pos := cube.Pos{origin.X() + x, origin.Y() + y, origin.Z() + z}
				tx.SetBlock(pos, b, nil)
			}
		}
	}
	return size, nil
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func writeInt32(w *bufio.Writer, v int32) { writeUint32(w, uint32(v)) }

func writeUint32(w *bufio.Writer, v uint32) {
	var buf [4]byte
	binary.LittleEndian.PutUint32(buf[:], v)
	w.Write(buf[:])
}

func readInt32(r *bufio.Reader) (int32, error) {
	v, err := readUint32(r)
	return int32(v), err
}

func readUint32(r *bufio.Reader) (uint32, error) {
	var buf [4]byte
	if _, err := readFull(r, buf[:]); err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint32(buf[:]), nil
}

func readFull(r *bufio.Reader, buf []byte) (int, error) {
	n := 0
	for n < len(buf) {
		m, err := r.Read(buf[n:])
		n += m
		if err != nil {
			return n, err
		}
	}
	return n, nil
}
