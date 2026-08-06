package schematics

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// ---------------------------------------------------------------------
// Storage location
// ---------------------------------------------------------------------
//
// PocketMine plugins each got their own plugin_data/<PluginName>/ folder for
// free. Dragonfly has no such concept — there's no "plugins" folder at all,
// just one process with one working directory (the same directory main.go
// already drops ranks.json, kb.json, etc. into, which on your Pterodactyl
// setup is the server's root/data volume).
//
// The replacement is a single top-level folder, schematics/, created next
// to the server binary on startup. Upload .schem files there over SFTP
// exactly like before — same server, same connection details from the
// Pterodactyl panel — just a different folder name than PocketMine used.

// Dir is the folder .schem files are read from and listed from. Call Init
// once at startup (see main.go) before any player can run /load.
var Dir = "schematics"

// Init ensures the schematics folder exists. Call this once in main(),
// alongside the other Load/Init calls.
func Init(dir string) error {
	Dir = dir
	return os.MkdirAll(Dir, 0755)
}

// List returns the names (without .schem extension) of every schematic
// currently sitting in Dir, sorted alphabetically — this is what //load
// with no argument prints.
func List() ([]string, error) {
	entries, err := os.ReadDir(Dir)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if strings.EqualFold(filepath.Ext(e.Name()), ".schem") {
			names = append(names, strings.TrimSuffix(e.Name(), filepath.Ext(e.Name())))
		}
	}
	sort.Strings(names)
	return names, nil
}

// resolvePath turns a bare name (no extension, no path separators — this is
// deliberately restrictive so a crafted "../../something" name can't read
// files outside Dir) into the .schem file path.
func resolvePath(name string) (string, error) {
	if strings.ContainsAny(name, `/\`) || strings.Contains(name, "..") {
		return "", fmt.Errorf("invalid schematic name")
	}
	return filepath.Join(Dir, name+".schem"), nil
}

// ---------------------------------------------------------------------
// Per-player load state
// ---------------------------------------------------------------------
//
// //load "name" only parses the file into memory — it does not touch the
// world. //paste then places whatever that player last loaded. This mirrors
// EasyEdit's SchematicLoadTask -> clipboard -> PasteCommand flow, minus
// everything else EasyEdit's clipboard system does (rotation, flipping,
// multiple named clipboards, etc.).

var (
	mu      sync.Mutex
	loaded  = map[string]*Schematic{} // XUID -> last loaded schematic
	loadedN = map[string]string{}     // XUID -> that schematic's name, for messages
)

// LoadForPlayer parses the named schematic and stores it as xuid's active
// clipboard, replacing any previous one.
func LoadForPlayer(xuid, name string) error {
	path, err := resolvePath(name)
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("no schematic named %q (run //load with no name to list them)", name)
	}
	s, err := Load(path)
	if err != nil {
		return err
	}
	mu.Lock()
	loaded[xuid] = s
	loadedN[xuid] = name
	mu.Unlock()
	return nil
}

// ActiveFor returns the schematic xuid last loaded, if any.
func ActiveFor(xuid string) (*Schematic, string, bool) {
	mu.Lock()
	defer mu.Unlock()
	s, ok := loaded[xuid]
	if !ok {
		return nil, "", false
	}
	return s, loadedN[xuid], true
}

// ---------------------------------------------------------------------
// Paste
// ---------------------------------------------------------------------

// PasteResult summarises what happened, for the message //paste sends back.
type PasteResult struct {
	Placed  int
	Skipped int
	// SkippedNames lists up to a handful of the distinct block names that
	// couldn't be translated, so the player/you can tell me exactly what to
	// add to translateBlock next.
	SkippedNames []string
}

// PasteInto places every block in s into tx, with s's local (0,0,0) corner
// positioned at origin. This is a direct, unbuffered write — there's no
// undo history (EasyEdit's history/ package is not part of this port), so
// double-check placement before confirming on a live world.
func PasteInto(tx *world.Tx, s *Schematic, origin cube.Pos) PasteResult {
	res := PasteResult{}
	seenSkipped := map[string]bool{}

	for ly := 0; ly < s.Height; ly++ {
		for lz := 0; lz < s.Length; lz++ {
			for lx := 0; lx < s.Width; lx++ {
				bs, ok := s.At(lx, ly, lz)
				if !ok {
					continue
				}
				b, ok := translateBlock(bs)
				if !ok {
					res.Skipped++
					if !seenSkipped[bs.Name] {
						seenSkipped[bs.Name] = true
						if len(res.SkippedNames) < 8 {
							res.SkippedNames = append(res.SkippedNames, bs.Name)
						}
					}
					continue
				}
				pos := cube.Pos{origin.X() + lx, origin.Y() + ly, origin.Z() + lz}
				tx.SetBlock(pos, b, nil)
				res.Placed++
			}
		}
	}
	return res
}

// translateBlock maps a Java Edition block state (from the .schem palette)
// to a Bedrock world.Block Dragonfly can place.
//
// ASSUMPTION FLAG: this leans on world.BlockByName(name string,
// properties map[string]any) (world.Block, bool) existing — that's the
// standard Dragonfly API for resolving a block by its namespaced ID (used
// internally by /setblock-style commands). I can't compile-check this
// against your exact pinned commit (there's no Go toolchain or network in
// my sandbox), so if this specific call is the thing that fails to build,
// paste me the exact compiler error and I'll fix just this function.
//
// Even when that call works, this is NOT a full Java->Bedrock converter —
// EasyEdit's real one (convert/block/*.php) is a hand-built mapping table
// covering hundreds of state differences between the two editions. Plain
// blocks (stone, dirt, planks, wool, concrete, logs, most simple furniture)
// share the same namespaced ID between editions and will paste correctly.
// Blocks whose property schema diverges between Java and Bedrock (stairs,
// some redstone components, a handful of renamed blocks) may come out
// wrong or get skipped. Send me the SkippedNames list after a paste and
// I'll extend this table for the specific blocks you actually use.
func translateBlock(bs BlockState) (world.Block, bool) {
	name := bs.Name
	if !strings.Contains(name, ":") {
		name = "minecraft:" + name
	}

	props := make(map[string]any, len(bs.Props))
	for k, v := range bs.Props {
		props[k] = javaPropValue(v)
	}

	if b, ok := world.BlockByName(name, props); ok {
		return b, true
	}
	// Retry with no properties — gets the block's default state instead of
	// failing outright just because e.g. a Java-only property didn't match.
	if b, ok := world.BlockByName(name, nil); ok {
		return b, true
	}
	return nil, false
}

// javaPropValue converts a raw Java block-state property string ("true",
// "3", "north", ...) into the Go type Dragonfly's property matching expects
// (bool/int/string), the same way NBT/JSON property values are typed.
func javaPropValue(v string) any {
	switch v {
	case "true":
		return true
	case "false":
		return false
	}
	if n, err := strconv.Atoi(v); err == nil {
		return n
	}
	return v
}
