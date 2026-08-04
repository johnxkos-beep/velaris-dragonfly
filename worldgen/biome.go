package worldgen

import (
	"fmt"
	"strings"

	"github.com/df-mc/dragonfly/server/world"
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
// findBiome panics if nothing registered matches any candidate, which
// means either the candidate list needs updating or (if world.Biomes() is
// empty entirely) biome registration isn't running at all - e.g. because
// nothing in the binary imports "github.com/df-mc/dragonfly/server".
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
	names := make([]string, len(all))
	for i, b := range all {
		names[i] = b.String()
	}
	panic(fmt.Sprintf("worldgen: no registered biome matched any of %v; %d biomes registered: %v", candidates, len(all), names))
}
