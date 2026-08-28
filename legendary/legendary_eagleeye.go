// Eagle Eye Bow's "freeze in the air while drawing" effect — confirmed
// real behavior from the add-on's own script (a function literally named
// `falling`, called every tick, that caps downward velocity to near-zero
// while the player is airborne and mid-draw with an arrow available). This
// file replicates that using the same Tick(tx)-gets-a-real-Tx entity
// pattern already proven safe elsewhere in this package (Projectile,
// hudBar) — no new unproven mechanism introduced.
package legendary

import (
	"log"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// EagleDrawTickerType is the entity type for the invisible draw/freeze
// ticker.
var EagleDrawTickerType eagleDrawTickerType

// EagleTypes returns the entity types this file adds, for merging into the
// server's entity registry — see the wiring note in main.go.
func EagleTypes() []world.EntityType { return []world.EntityType{EagleDrawTickerType} }

var eagleDrawBBox = cube.Box(0, 0, 0, 0, 0, 0)

type eagleDrawTickerType struct{}

func (eagleDrawTickerType) EncodeEntity() string        { return "bey:eagle_draw_ticker" }
func (eagleDrawTickerType) BBox(world.Entity) cube.BBox { return eagleDrawBBox }
func (eagleDrawTickerType) Open(tx *world.Tx, handle *world.EntityHandle, data *world.EntityData) world.Entity {
	st, ok := data.Data.(*eagleDrawState)
	if !ok || st == nil {
		st = &eagleDrawState{}
		data.Data = st
	}
	return &eagleDrawTicker{tx: tx, handle: handle, data: data, state: st}
}
func (eagleDrawTickerType) DecodeNBT(m map[string]any, data *world.EntityData) {
	data.Data = &eagleDrawState{}
}
func (eagleDrawTickerType) EncodeNBT(data *world.EntityData) map[string]any { return map[string]any{} }

type eagleDrawState struct {
	Owner *world.EntityHandle
}

// EagleDrawConfig configures a newly spawned draw ticker.
type EagleDrawConfig struct {
	Owner *world.EntityHandle
}

func (c EagleDrawConfig) Apply(data *world.EntityData) {
	data.Data = &eagleDrawState{Owner: c.Owner}
}

// spawnEagleDrawTicker starts the freeze-while-drawing effect for p. Runs
// every tick until playerAbilityState.eagleDrawing goes false (set by
// OnRelease) or the owner stops holding the Eagle Eye Bow, then despawns.
func spawnEagleDrawTicker(tx *world.Tx, p *player.Player) {
	handle := world.EntitySpawnOpts{Position: p.Position()}.New(EagleDrawTickerType, EagleDrawConfig{Owner: p.H()})
	tx.AddEntity(handle)
}

type eagleDrawTicker struct {
	tx     *world.Tx
	handle *world.EntityHandle
	data   *world.EntityData
	state  *eagleDrawState
}

func (e *eagleDrawTicker) H() *world.EntityHandle  { return e.handle }
func (e *eagleDrawTicker) Position() mgl64.Vec3    { return e.data.Pos }
func (e *eagleDrawTicker) Rotation() cube.Rotation { return e.data.Rot }
func (e *eagleDrawTicker) Close() error            { return nil }

func (e *eagleDrawTicker) Tick(tx *world.Tx, current int64) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[legendary] recovered panic in eagleDrawTicker.Tick: %v", r)
		}
	}()
	owner, ok := e.state.Owner.Entity(tx)
	if !ok {
		tx.RemoveEntity(e)
		return
	}
	p, ok := owner.(*player.Player)
	if !ok {
		tx.RemoveEntity(e)
		return
	}

	s := stateFor(p.XUID())
	s.mu.Lock()
	drawing := s.eagleDrawing
	s.mu.Unlock()
	if !drawing {
		tx.RemoveEntity(e)
		return
	}

	held, _ := p.HeldItems()
	w, ok := held.Item().(legendaryItem)
	if !ok || w.WeaponDef().ID != "bey:eagle_eye_bow" {
		// Switched away mid-draw — stop freezing them, but leave
		// eagleDrawing itself alone (OnRelease still resolves it if/when
		// it fires; if the player never lets go, this ticker despawning
		// just means the freeze stops, which is the right visual either
		// way).
		tx.RemoveEntity(e)
		return
	}

	if !p.OnGround() {
		// UNVERIFIED: assumes *player.Player has a Velocity() mgl64.Vec3
		// getter alongside the already-confirmed SetVelocity — a very
		// standard pairing, but not independently confirmed from this
		// environment. If it doesn't exist under this name, it's a
		// one-line fix (the rest of the freeze logic doesn't depend on
		// exactly how the current velocity was read).
		vel := p.Velocity()
		if vel.Y() < -freezeMaxFall {
			p.SetVelocity(mgl64.Vec3{vel.X(), -freezeMaxFall, vel.Z()})
		}
	}
}
