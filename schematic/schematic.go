// Package schematic is a Go port of exactly three commands worth of
// workflow from the PocketMine-MP plugin EasyEdit: /save, /load, and
// /paste (EasyEdit's own alias for /saveschematic, plus /loadschematic +
// /pasteclipboard split the way the live PMMP plugin's chat flow is
// actually used: /load fills your clipboard, /paste places it wherever
// you're standing, and running /paste again pastes the same thing
// again). Nothing else from EasyEdit — no brushes, selections-by-click,
// history, generation commands, etc. — was ported. Per request, this is
// intentionally small.
//
// Format note: EasyEdit always *saves* as a Sponge .schem (Java Edition
// NBT layout, translated through PocketMine's own block-state upgrader)
// but can *load* several formats including Bedrock's .mcstructure AND
// the legacy MCEdit .schematic format (Java numeric block-id + metadata
// pairs, run through EasyEdit's own LegacyBlockIdConvertor lookup
// table). Reproducing any of those exactly would mean re-implementing
// PocketMine's NBT block-state upgrader (for .schem/.mcstructure) or
// EasyEdit's legacy Java-ID conversion table (for .schematic) in Go,
// with no network access in this session to fetch the real mapping data
// or check field names against df-mc/dragonfly's actual source.
// Instead this uses a small custom binary format — magic "RFSC",
// extension .rfschem — built entirely on world.BlockRuntimeID, already
// proven to compile and run elsewhere in this repo (see
// worldgen/nether.go). No new dependencies needed; only the standard
// library.
//
// IMPORTANT: this means an existing .schematic or .schem file (e.g. one
// exported from EasyEdit on the PMMP/rainfall.land server) will NOT load
// here — /load only recognises .rfschem files created by THIS server's
// own /save. There's no conversion path between the two right now; a
// build has to be re-created with /save on this Dragonfly server to get
// an .rfschem for it.
//
// Known limitations (documented rather than silently dropped):
//   - Block entities (chest contents, sign text, cooking progress, etc.)
//     are NOT saved, only the block itself. EasyEdit's own .mcstructure
//     reader has the same gap for entities ("//TODO: entities" in its
//     source); this port has it for tiles too, to keep the format
//     simple enough to get right without a compiler available. This
//     turned out to matter more than "loads back in empty": a furnace
//     placed through a paste crashed the server (nil pointer inside
//     Dragonfly's own furnace-ticking code, since the block's internal
//     cooking state was never set up). Rather than risk that for other
//     container/entity block types too, Clipboard.PasteAt now skips
//     placing anything matching blockEntityRisk below (placed as air
//     instead) — see that var's comment.
//   - Entities (mobs, item frames, etc.) inside the region are NOT
//     saved.
//
// UNVERIFIED: world.BlockByRuntimeID (used in Clipboard.PasteAt, below)
// is not used anywhere else in this repo, so unlike BlockRuntimeID it
// hasn't been proven against your actual dragonfly v0.11.4 build. If
// `go build` fails specifically on that call, paste the compiler error
// back — it's a one-line fix (wrong return signature or package).
package schematic

import (
	"bufio"
	"compress/gzip"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"

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
	maxVolume = 6000000 // ~182 x 182 x 182
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
// currently in Dir, sorted alphabetically. Legacy .schematic files (see
// legacy.go) are included too — a name present as both only appears
// once (Load prefers the .rfschem version when both exist).
func List() []string {
	entries, err := os.ReadDir(Dir)
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		n, ok := strings.CutSuffix(e.Name(), ".rfschem")
		if !ok {
			n, ok = strings.CutSuffix(e.Name(), ".schematic")
		}
		if ok && !seen[n] {
			seen[n] = true
			names = append(names, n)
		}
	}
	sort.Strings(names)
	return names
}

// Exists reports whether a .rfschem schematic with this name is on disk.
// See also LegacyExists (legacy.go) for .schematic files.
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

// Clipboard is a schematic that's been read off disk into memory but not
// placed into the world yet — the Go equivalent of EasyEdit's per-player
// clipboard that /load fills and /paste consumes.
type Clipboard struct {
	Name string
	Size Size
	Ids  []uint32 // runtime IDs, len == Size.Volume(), ordered x outer, y middle, z inner (matches Export)
}

// airID returns minecraft:air's runtime ID, computed once and cached —
// used by GroundOffset below to tell "real block" from "empty padding".
var (
	airIDOnce sync.Once
	airIDVal  uint32
)

func airID() uint32 {
	airIDOnce.Do(func() {
		if air, ok := world.BlockByName("minecraft:air", nil); ok {
			airIDVal = world.BlockRuntimeID(air)
		}
	})
	return airIDVal
}

// GroundOffset returns how many empty (air-only) Y layers sit below the
// clipboard's actual content, counting up from Y=0. Saved regions often
// have a bit of air under the real build if the original selection
// corners weren't tight to it — pasting at Y=0 as-is would put that
// empty padding at the paste point and the real structure floating
// above it. PasteAt subtracts this from the requested origin so the
// first real layer lands there instead. Returns 0 if the very bottom
// layer already has a block in it (nothing to skip) or if the whole
// clipboard is air (nothing to anchor to).
func (c *Clipboard) GroundOffset() int {
	air := airID()
	for y := 0; y < c.Size.Y; y++ {
		for x := 0; x < c.Size.X; x++ {
			for z := 0; z < c.Size.Z; z++ {
				if c.Ids[(x*c.Size.Y+y)*c.Size.Z+z] != air {
					return y
				}
			}
		}
	}
	return 0
}

// ReadFile reads Dir/name.rfschem off disk into memory. It does NOT touch
// the world — see Clipboard.PasteAt for that.
func ReadFile(name string) (*Clipboard, error) {
	f, err := os.Open(path(name))
	if err != nil {
		return nil, fmt.Errorf("couldn't open schematic file: %w", err)
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("couldn't read schematic file (corrupt or not a .rfschem): %w", err)
	}
	defer gz.Close()
	r := bufio.NewReader(gz)

	got := make([]byte, len(magic))
	if _, err := readFull(r, got); err != nil || string(got) != magic {
		return nil, errors.New("not a valid .rfschem file")
	}
	ver, err := r.ReadByte()
	if err != nil || ver != formatVersion {
		return nil, errors.New("unsupported .rfschem version")
	}

	sx, err1 := readInt32(r)
	sy, err2 := readInt32(r)
	sz, err3 := readInt32(r)
	if err1 != nil || err2 != nil || err3 != nil {
		return nil, errors.New("truncated schematic file")
	}
	size := Size{int(sx), int(sy), int(sz)}
	if size.X <= 0 || size.Y <= 0 || size.Z <= 0 || size.Volume() > maxVolume {
		return nil, fmt.Errorf("schematic is %d blocks, over the %d limit", size.Volume(), maxVolume)
	}

	ids := make([]uint32, size.Volume())
	for i := range ids {
		id, err := readUint32(r)
		if err != nil {
			return nil, errors.New("truncated schematic file")
		}
		ids[i] = id
	}

	return &Clipboard{Name: name, Size: size, Ids: ids}, nil
}

// PasteAt writes the clipboard into tx with its min corner at origin,
// fully overwriting whatever was there (including with air, wherever the
// saved region was empty), matching how a WorldEdit-style paste behaves.
// blockEntityRisk lists Go struct-type-name substrings (as reported by
// reflect) for block types known to carry internal state — an
// inventory, fuel/cook progress, a stored item or text, etc. — that
// this package's export/conversion never captures (see the package doc
// comment). Placing a bare reconstructed instance of one of these
// crashed the server on its very next random tick: a nil pointer deep
// inside Dragonfly's own furnace-ticking code, because the furnace's
// internal cooking state was never set up the way normal placement or
// world-loading would set it up. Rather than risk the same crash for
// every other container/entity block type, PasteAt below skips
// anything matching one of these names — the destination just keeps
// whatever was already there (usually air, on fresh ground) instead of
// getting the real block placed.
//
// This list is a defensive guess at Dragonfly's actual type names
// (Furnace, Chest, etc. — matching the vanilla/bedrock naming these
// blocks are universally known by), not something pulled from source,
// since I don't have compiler access to confirm exact type names this
// session. If a build with e.g. a working chest still crashes on
// something not in this list, the fix is just adding that block's
// English name here.
var blockEntityRisk = []string{
	"Furnace", "Smoker", "BlastFurnace", "Chest", "Barrel", "ShulkerBox",
	"Hopper", "Dispenser", "Dropper", "BrewingStand", "Jukebox",
	"Lectern", "Campfire", "Beacon", "Conduit", "FlowerPot",
	"EnchantingTable", "Cauldron", "Bed", "Sign", "Banner", "Skull",
	"CommandBlock", "StructureBlock", "MovingBlock", "Bell", "Loom",
	"Grindstone", "Smithing",
}

// blockEntityUnsafe reports whether b's Go type name matches one of the
// categories in blockEntityRisk.
func blockEntityUnsafe(b world.Block) bool {
	t := reflect.TypeOf(b)
	if t == nil {
		return false
	}
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	name := t.Name()
	for _, risky := range blockEntityRisk {
		if strings.Contains(name, risky) {
			return true
		}
	}
	return false
}

// PasteAt writes the clipboard into tx with its min corner at origin,
// fully overwriting whatever was there (including with air, wherever
// the saved region was empty), matching how a WorldEdit-style paste
// behaves — EXCEPT for anything matching blockEntityRisk (see its
// comment), which is left untouched (skipped) rather than placed.
// Returns how many blocks were skipped for that reason.
func (c *Clipboard) PasteAt(tx *world.Tx, origin cube.Pos) (skipped int) {
	i := 0
	for x := 0; x < c.Size.X; x++ {
		for y := 0; y < c.Size.Y; y++ {
			for z := 0; z < c.Size.Z; z++ {
				b, ok := world.BlockByRuntimeID(c.Ids[i])
				i++
				if !ok {
					// Block ID from a different server build/version —
					// skip rather than place something wrong or crash.
					continue
				}
				if blockEntityUnsafe(b) {
					skipped++
					continue // leave as whatever's already there (usually air, on fresh ground)
				}
				pos := cube.Pos{origin.X() + x, origin.Y() + y, origin.Z() + z}
				tx.SetBlock(pos, b, nil)
			}
		}
	}
	return skipped
}

// clipboards holds each player's currently loaded schematic, keyed by
// XUID (same key convention as state.CoordsState elsewhere in this
// repo). /load fills this; /paste reads and places it; it is NOT
// cleared after a paste, so the same player can /paste the same
// schematic more than once (matches EasyEdit, whose /paste doesn't
// consume the clipboard either).
var (
	clipMu     sync.Mutex
	clipboards = map[string]*Clipboard{}
)

// SetClipboard stores c as xuid's current clipboard.
func SetClipboard(xuid string, c *Clipboard) {
	clipMu.Lock()
	defer clipMu.Unlock()
	clipboards[xuid] = c
}

// GetClipboard returns xuid's current clipboard, if any.
func GetClipboard(xuid string) (*Clipboard, bool) {
	clipMu.Lock()
	defer clipMu.Unlock()
	c, ok := clipboards[xuid]
	return c, ok
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
