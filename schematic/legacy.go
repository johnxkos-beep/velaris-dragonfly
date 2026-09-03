package schematic

import (
	"compress/gzip"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/nbt"
)

// This file adds support for reading legacy MCEdit .schematic files (the
// format your existing koth.schematic is in) into the same Clipboard type
// .rfschem files load into — so /load "koth" works whether koth.rfschem
// or koth.schematic is what's sitting in Dir.
//
// Where the mapping data came from: EasyEdit's own converter
// (LegacyBlockIdConvertor.php) doesn't ship this table inside the plugin —
// it downloads it at runtime from
// https://github.com/platz1de/EasyEdit-Data (format-v3.1 branch,
// legacy-conversion-map.json), which is the URL EasyEdit's default
// config.yml points at. I fetched that file directly rather than
// guessing at IDs.
//
// COVERAGE GAP — read this before relying on it: that file is 1684 lines;
// GitHub's page only rendered roughly the first 1000 of them (java block
// IDs 0 through 149) before requiring JavaScript to load more, and the
// raw file endpoint refused automated fetches (robots.txt). So
// legacy_ids.json here is a genuine excerpt of the real table (not
// invented), but only for IDs 0–146, and for blocks with many metadata
// variants (fire, crops, skulls, etc.) I kept only the common state(s) to
// keep the file a sane size — the full picture is at
// https://github.com/platz1de/EasyEdit-Data/blob/format-v3.1/legacy-conversion-map.json
// if you want to fill in more by hand later (same "id:meta": "bedrockState"
// format, appended into legacy_ids.json).
//
// Any java id:meta pair NOT in the table falls back to air rather than
// guessing wrong — see ReadMcEditSchematic's returned unknown count,
// which /load reports to the player so silent gaps are visible instead
// of hidden.
//
// UNVERIFIED: the nbt package calls here (nbt.NewDecoderWithEncoding,
// nbt.BigEndian, Decoder.Decode) are gophertunnel APIs I'm confident
// about but haven't compiled against your pinned version. If `go build`
// fails on this file specifically, paste the error back.

//go:embed legacy_ids.json
var legacyIDsJSON []byte

var legacyMap map[string]string

func init() {
	legacyMap = map[string]string{}
	if err := json.Unmarshal(legacyIDsJSON, &legacyMap); err != nil {
		// Should be impossible (the JSON is embedded at build time), but
		// fail soft rather than panic at server startup.
		legacyMap = map[string]string{}
	}
}

// legacyPath returns the on-disk path for a .schematic file, using the
// same sanitisation as path() in schematic.go.
func legacyPath(name string) string {
	name = filepath.Base(strings.TrimSuffix(name, ".schematic"))
	return filepath.Join(Dir, name+".schematic")
}

// LegacyExists reports whether a .schematic file with this name is on disk.
func LegacyExists(name string) bool {
	_, err := os.Stat(legacyPath(name))
	return err == nil
}

// mcEditSchematic is the subset of the classic MCEdit .schematic NBT
// layout this reads: block IDs + metadata only. Tile entities (chest
// contents, sign text) and entities are not read — same documented gap
// as the rest of this package.
type mcEditSchematic struct {
	Width  int16  `nbt:"Width"`
	Height int16  `nbt:"Height"`
	Length int16  `nbt:"Length"`
	Blocks []byte `nbt:"Blocks"`
	Data   []byte `nbt:"Data"`
}

// ReadMcEditSchematic reads Dir/name.schematic, converts every block
// through the legacy Java-ID table, and returns it as a Clipboard (same
// type ReadFile returns for .rfschem) — so it can be handed to
// SetClipboard/PasteAt identically. The second return value is how many
// blocks had no entry in legacy_ids.json and were placed as air.
func ReadMcEditSchematic(name string) (*Clipboard, int, error) {
	f, err := os.Open(legacyPath(name))
	if err != nil {
		return nil, 0, fmt.Errorf("couldn't open .schematic file: %w", err)
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, 0, fmt.Errorf("couldn't decompress .schematic file: %w", err)
	}
	defer gz.Close()

	var s mcEditSchematic
	if err := nbt.NewDecoderWithEncoding(gz, nbt.BigEndian).Decode(&s); err != nil {
		return nil, 0, fmt.Errorf("couldn't parse .schematic NBT (not a valid MCEdit schematic?): %w", err)
	}

	size := Size{int(s.Width), int(s.Height), int(s.Length)}
	if size.X <= 0 || size.Y <= 0 || size.Z <= 0 || size.Volume() > maxVolume {
		return nil, 0, fmt.Errorf(".schematic is %d blocks (or has an invalid size) — over the %d limit", size.Volume(), maxVolume)
	}
	if len(s.Blocks) < size.Volume() || len(s.Data) < size.Volume() {
		return nil, 0, errors.New(".schematic block data is shorter than its declared size — file may be corrupt")
	}

	air, ok := world.BlockByName("minecraft:air", nil)
	if !ok {
		return nil, 0, errors.New("minecraft:air not found via world.BlockByName — something is very wrong with this build")
	}
	airID := world.BlockRuntimeID(air)

	// Resolved (name, properties) -> runtime ID is repeated a LOT (every
	// stone block hits the same lookup), so cache it instead of calling
	// world.BlockByName per-block.
	resolved := map[string]uint32{}

	ids := make([]uint32, size.Volume())
	unknown := 0

	// MCEdit's own order (confirmed against EasyEdit's own reader):
	// y outer, z middle, x inner.
	i := 0
	for y := 0; y < size.Y; y++ {
		for z := 0; z < size.Z; z++ {
			for x := 0; x < size.X; x++ {
				blockID := int(s.Blocks[i])
				meta := int(s.Data[i]) & 0xF
				i++

				// Our own Ids slice uses x-outer, y-middle, z-inner
				// order (matches Export/PasteAt in schematic.go).
				outIdx := (x*size.Y+y)*size.Z + z

				key := strconv.Itoa(blockID) + ":" + strconv.Itoa(meta)
				rid, cached := resolved[key]
				if !cached {
					state, found := legacyMap[key]
					if !found {
						ids[outIdx] = airID
						unknown++
						continue
					}
					bName, props := parseBlockState(state)
					b, ok := world.BlockByName(bName, props)
					if !ok {
						ids[outIdx] = airID
						unknown++
						resolved[key] = airID
						continue
					}
					rid = world.BlockRuntimeID(b)
					resolved[key] = rid
				}
				ids[outIdx] = rid
			}
		}
	}

	return &Clipboard{Name: name, Size: size, Ids: ids}, unknown, nil
}

// parseBlockState parses a bedrock state string like
// "minecraft:chest[cardinal_direction=north]" into a block name and a
// properties map suitable for world.BlockByName. Bare "true"/"false"
// become bool, purely numeric values become int32, everything else
// stays a string.
func parseBlockState(s string) (string, map[string]any) {
	name := s
	props := map[string]any{}

	if i := strings.IndexByte(s, '['); i >= 0 && strings.HasSuffix(s, "]") {
		name = s[:i]
		body := s[i+1 : len(s)-1]
		if body != "" {
			for _, pair := range strings.Split(body, ",") {
				kv := strings.SplitN(pair, "=", 2)
				if len(kv) != 2 {
					continue
				}
				key := strings.TrimPrefix(kv[0], "minecraft:")
				props[key] = parseStateValue(kv[1])
			}
		}
	}
	return name, props
}

func parseStateValue(v string) any {
	switch v {
	case "true":
		return true
	case "false":
		return false
	}
	if n, err := strconv.Atoi(v); err == nil {
		return int32(n)
	}
	return v
}
