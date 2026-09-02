package mobs

// TEMPORARY — delete this file once you've confirmed the package builds.
//
// This line forces the Go compiler to check whether *Mob satisfies
// Dragonfly's internal entity.Living interface (the thing Player.AttackEntity
// requires before it'll deal any damage to a custom entity). If Mob is
// missing something, the build will fail with an error that lists exactly
// which method(s) are missing or have the wrong signature — paste that
// error back and it's a precise, one-pass fix instead of more guessing.
import "github.com/df-mc/dragonfly/server/entity"

var _ entity.Living = (*Mob)(nil)

// Same check for the new hostile mobs (zombie/skeleton/spider/creeper) —
// added alongside them so any interface-satisfaction problem shows up
// immediately instead of only when a player first attacks one.
var _ entity.Living = (*HostileMob)(nil)
