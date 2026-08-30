// Eagle Eye Bow's real draw-and-release + mid-air-freeze mechanic used to
// live in this file. It's been reverted per explicit request — Eagle Eye
// Bow is back to the simple instant-fire version (see shootEagleEyeBow in
// abilities.go). EagleTypes is kept as a no-op (empty) so main.go's entity
// registry merge doesn't need editing; this whole file can be deleted
// later if you're sure you don't want to revisit the draw mechanic.
package legendary

import "github.com/df-mc/dragonfly/server/world"

// EagleTypes returns the entity types this file used to add — currently
// none, since the draw/freeze ticker entity was removed along with the
// feature it supported.
func EagleTypes() []world.EntityType { return nil }
