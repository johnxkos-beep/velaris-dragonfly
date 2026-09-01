package dfworlds

import (
	"context"
	"errors"
	"testing"

	"github.com/df-mc/dragonfly/server/player"
	dfworld "github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func TestRouterSendDefaultUsesConfiguredDestination(t *testing.T) {
	src := testWorld(t)
	dst := testWorld(t)
	defer func() {
		_ = src.Close()
		_ = dst.Close()
	}()

	m := New(Config{Root: t.TempDir(), Log: discardLogger()})
	spawn := Spawn{Position: mgl64.Vec3{0.5, 80, 0.5}}
	if _, err := m.RegisterWorld(Definition{Name: "overworld", Spawn: &spawn}, dst); err != nil {
		t.Fatalf("RegisterWorld() error = %v", err)
	}
	router, err := NewRouter(m, "overworld")
	if err != nil {
		t.Fatalf("NewRouter() error = %v", err)
	}

	handle := dfworld.EntitySpawnOpts{Position: mgl64.Vec3{4, 64, 4}}.New(player.Type, player.Config{})
	runWorld(t, src, func(tx *dfworld.Tx) {
		tx.AddEntity(handle)
	})

	if err := router.SendDefaultHandle(context.Background(), handle); err != nil {
		t.Fatalf("SendDefaultHandle() error = %v", err)
	}

	inDestination := callEntity(t, handle, func(tx *dfworld.Tx, e dfworld.Entity) (bool, error) {
		p := e.(*player.Player)
		return tx.World() == dst && p.Position() == spawn.Position, nil
	})
	if !inDestination {
		t.Fatal("player handle was not sent to the default destination")
	}
}

func TestNewRouterRejectsNilManager(t *testing.T) {
	if _, err := NewRouter(nil, "overworld"); !errors.Is(err, ErrNilManager) {
		t.Fatalf("NewRouter(nil) error = %v, want ErrNilManager", err)
	}
}
