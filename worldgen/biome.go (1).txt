package worldgen

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"

	"github.com/df-mc/dragonfly/server/world"
)

// logGenPanic reports a recovered panic from chunk generation to stderr
// (so it shows up in the Pterodactyl console/log) without taking the
// server down. dim is "overworld" or "nether"; pos is the chunk that was
// being generated when it happened.
func logGenPanic(dim string, pos world.ChunkPos, r any) {
	slog.New(slog.NewTextHandler(os.Stderr, nil)).Error(
		"worldgen: recovered panic generating chunk (server stayed up)",
		"dimension", dim,
		"chunkX", pos.X(),
		"chunkZ", pos.Z(),
		"panic", fmt.Sprintf("%v", r),
	)
}

// warnOnce makes sure each distinct "no match" case is only logged the
// first time it happens, so a biome that generates thousands of times a
// second doesn't spam the console.
var (
	warnOnceMu sync.Mutex
	warnOnce   = map[string]bool{}
)

// findBiome looks through every biome actually registered with the world
// package and returns the first one whose name contains any of the given
// substrings (case-insensitive). Candidates are tried in order, so put the
// most specific/likely name first.
//
// This deliberately avoids hardcoding exact biome name strings such as
// "minecraft:extreme_hills": those have changed between dragonfly versions
// (renamed to "minecraft:windswept_hills" to track vanilla, and in some
// builds the "minecraft:" namespace prefix is dropped entirely). Matching
// by substring against whatever is actually registered is resilient to
// both kinds of change.
//
// findBiome used to panic if nothing registered matched any candidate.
// That's what was crashing the whole server (kicking every connected
// player, not just the one standing on the offending chunk) the moment
// GenerateChunk ran on a never-before-loaded chunk that picked a biome
// bucket (e.g. hills/dark forest/snowy) whose candidate names didn't
// match anything this Dragonfly build has registered. A single missed
// biome name should never be able to take the whole process down, so
// this now logs once and falls back to the first registered biome
// instead of panicking. If you see the fallback warning in the console,
// update the candidate list for that bucket with whatever name it logs.
func findBiome(candidates ...string) world.Biome {
	all := world.Biomes()
	for _, want := range candidates {
		want = strings.ToLower(want)
		for _, b := range all {
			if strings.Contains(strings.ToLower(b.String()), want) {
				return b
			}
		}
	}
	key := strings.Join(candidates, ",")
	warnOnceMu.Lock()
	alreadyWarned := warnOnce[key]
	warnOnce[key] = true
	warnOnceMu.Unlock()
	if !alreadyWarned {
		names := make([]string, len(all))
		for i, b := range all {
			names[i] = b.String()
		}
		slog.New(slog.NewTextHandler(os.Stderr, nil)).Error(
			"worldgen: no registered biome matched any candidate, falling back",
			"candidates", fmt.Sprintf("%v", candidates),
			"registered", fmt.Sprintf("%v", names),
		)
	}
	if len(all) == 0 {
		panic("worldgen: no biomes registered at all - is github.com/df-mc/dragonfly/server imported?")
	}
	return all[0]
}
