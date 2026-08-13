package demonking

// TEMPORARY — delete this file once the real bug is fixed.
//
// This line forces the Go compiler to check whether *DemonKing satisfies
// Dragonfly's internal entity.Living interface (the thing Player.AttackEntity
// requires before it'll deal any damage to a custom entity). If DemonKing is
// missing something, the build will fail with an error that lists exactly
// which method(s) are missing or have the wrong signature — paste that error
// back and it's a precise, one-pass fix instead of more guessing.
import "github.com/df-mc/dragonfly/server/entity"

var _ entity.Living = (*DemonKing)(nil)
