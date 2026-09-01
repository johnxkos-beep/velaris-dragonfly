package dfworlds

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/player"
	dfworld "github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func TestLoadAllLoadsWorldDirectories(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"free", "arena"} {
		if err := os.Mkdir(filepath.Join(root, name), 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "README.txt"), []byte("ignored"), 0644); err != nil {
		t.Fatal(err)
	}

	m := New(Config{Root: root, Log: discardLogger()})
	defer func() {
		if err := m.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	loaded, err := m.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll() error = %v", err)
	}
	if want := []string{"arena", "free"}; !reflect.DeepEqual(loaded, want) {
		t.Fatalf("LoadAll() = %v, want %v", loaded, want)
	}
	if want := []string{"arena", "free"}; !reflect.DeepEqual(m.Names(), want) {
		t.Fatalf("Names() = %v, want %v", m.Names(), want)
	}
	if _, ok := m.World("FREE"); !ok {
		t.Fatal("World(\"FREE\") returned no world")
	}
}

func TestLoadRejectsUnsafeWorldNames(t *testing.T) {
	m := New(Config{Root: t.TempDir(), Log: discardLogger()})
	for _, name := range []string{"", ".", "..", "a/b", `a\b`} {
		if _, err := m.Load(name); !errors.Is(err, ErrInvalidName) {
			t.Fatalf("Load(%q) error = %v, want ErrInvalidName", name, err)
		}
	}
}

func TestRegisterDoesNotTakeOwnership(t *testing.T) {
	w := testWorld(t)
	defer func() {
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	m := New(Config{Root: t.TempDir(), Log: discardLogger()})
	if err := m.Register("external", w); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if err := m.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	task := w.Do(func(*dfworld.Tx) {})
	select {
	case <-task.Done():
		if err := task.Err(); err != nil {
			t.Fatalf("registered world task failed: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("registered world was closed by manager")
	}
}

func TestOpenAppliesDefinitionMetadata(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "arena-map"), 0755); err != nil {
		t.Fatal(err)
	}

	m := New(Config{Root: root, Log: discardLogger()})
	defer func() {
		if err := m.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	spawn := Spawn{Position: mgl64.Vec3{12.5, 72, -4.5}, Rotation: cube.Rotation{90, -15}}
	loaded, err := m.Open(Definition{
		Name:     "arena",
		Folder:   "arena-map",
		Spawn:    &spawn,
		GameMode: dfworld.GameModeAdventure,
	})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if loaded.Name != "arena" || loaded.Folder != "arena-map" || loaded.Path != filepath.Join(root, "arena-map") {
		t.Fatalf("Open() loaded metadata = %#v", loaded)
	}
	if loaded.Spawn != spawn {
		t.Fatalf("Open() spawn = %#v, want %#v", loaded.Spawn, spawn)
	}
	if loaded.GameMode != dfworld.GameModeAdventure {
		t.Fatalf("Open() game mode = %T, want adventure", loaded.GameMode)
	}
	if got := loaded.World.Spawn(); got != cube.PosFromVec3(spawn.Position) {
		t.Fatalf("world spawn = %v, want %v", got, cube.PosFromVec3(spawn.Position))
	}
	if got := loaded.World.DefaultGameMode(); got != dfworld.GameModeAdventure {
		t.Fatalf("world default game mode = %T, want adventure", got)
	}
}

func TestTransferHandleMovesEntityBetweenWorlds(t *testing.T) {
	src := testWorld(t)
	dst := testWorld(t)
	defer func() {
		_ = src.Close()
		_ = dst.Close()
	}()

	handle := dfworld.EntitySpawnOpts{Position: mgl64.Vec3{1, 64, 1}}.New(testEntityType{}, testEntityConfig{})
	runWorld(t, src, func(tx *dfworld.Tx) {
		tx.AddEntity(handle)
	})

	if err := TransferHandle(context.Background(), handle, dst); err != nil {
		t.Fatalf("TransferHandle() error = %v", err)
	}

	inDestination := callEntity(t, handle, func(tx *dfworld.Tx, _ dfworld.Entity) (bool, error) {
		return tx.World() == dst, nil
	})
	if !inDestination {
		t.Fatal("entity handle was not moved to destination world")
	}
}

func TestTravelPlayerHandleAppliesDestinationDefaults(t *testing.T) {
	src := testWorld(t)
	dst := testWorld(t)
	defer func() {
		_ = src.Close()
		_ = dst.Close()
	}()

	spawn := Spawn{Position: mgl64.Vec3{8.5, 65, -3.5}, Rotation: cube.Rotation{135, -10}}
	m := New(Config{Root: t.TempDir(), Log: discardLogger()})
	if _, err := m.RegisterWorld(Definition{
		Name:     "arena",
		Spawn:    &spawn,
		GameMode: dfworld.GameModeAdventure,
	}, dst); err != nil {
		t.Fatalf("RegisterWorld() error = %v", err)
	}

	handle := dfworld.EntitySpawnOpts{Position: mgl64.Vec3{1, 64, 1}}.New(player.Type, player.Config{
		GameMode: dfworld.GameModeSurvival,
	})
	runWorld(t, src, func(tx *dfworld.Tx) {
		tx.AddEntity(handle)
	})

	var before, after bool
	err := m.TravelPlayerHandle(context.Background(), handle, "arena",
		BeforeTravel(func(tx *dfworld.Tx, p *player.Player) error {
			before = tx.World() == src && p.GameMode() == dfworld.GameModeSurvival
			return nil
		}),
		AfterTravel(func(tx *dfworld.Tx, p *player.Player) error {
			after = tx.World() == dst &&
				p.Position() == spawn.Position &&
				p.Rotation() == spawn.Rotation &&
				p.GameMode() == dfworld.GameModeAdventure
			return nil
		}),
	)
	if err != nil {
		t.Fatalf("TravelPlayerHandle() error = %v", err)
	}
	if !before || !after {
		t.Fatalf("travel hooks before=%v after=%v, want both true", before, after)
	}

	inDestination := callEntity(t, handle, func(tx *dfworld.Tx, _ dfworld.Entity) (bool, error) {
		return tx.World() == dst, nil
	})
	if !inDestination {
		t.Fatal("player handle was not moved to destination world")
	}
}

func TestTransferPlayerHandleHonoursCancelledContext(t *testing.T) {
	src := testWorld(t)
	dst := testWorld(t)
	defer func() {
		_ = src.Close()
		_ = dst.Close()
	}()

	handle := dfworld.EntitySpawnOpts{Position: mgl64.Vec3{1, 64, 1}}.New(player.Type, player.Config{})
	runWorld(t, src, func(tx *dfworld.Tx) {
		tx.AddEntity(handle)
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := TransferPlayerHandle(ctx, handle, dst, mgl64.Vec3{8, 70, 8}); !errors.Is(err, context.Canceled) {
		t.Fatalf("TransferPlayerHandle() error = %v, want context.Canceled", err)
	}

	inSource := callEntity(t, handle, func(tx *dfworld.Tx, _ dfworld.Entity) (bool, error) {
		return tx.World() == src, nil
	})
	if !inSource {
		t.Fatal("cancelled transfer moved the player out of the source world")
	}
}

func TestTransferPlayerHandleRestoresPlayerAfterDestinationCloses(t *testing.T) {
	src := testWorld(t)
	dst := testWorld(t)
	defer func() {
		_ = src.Close()
		_ = dst.Close()
	}()

	handle := dfworld.EntitySpawnOpts{Position: mgl64.Vec3{1, 64, 1}}.New(player.Type, player.Config{})
	runWorld(t, src, func(tx *dfworld.Tx) {
		tx.AddEntity(handle)
	})
	if err := dst.Close(); err != nil {
		t.Fatalf("close destination: %v", err)
	}

	err := TransferPlayerHandle(context.Background(), handle, dst, mgl64.Vec3{8, 70, 8})
	if !errors.Is(err, dfworld.ErrWorldClosed) {
		t.Fatalf("TransferPlayerHandle() error = %v, want world.ErrWorldClosed", err)
	}

	inSource := callEntity(t, handle, func(tx *dfworld.Tx, _ dfworld.Entity) (bool, error) {
		return tx.World() == src, nil
	})
	if !inSource {
		t.Fatal("failed transfer did not restore the player to the source world")
	}
}

func TestTravelPlayerTxMovesPlayerFromOpenTransaction(t *testing.T) {
	src := testWorld(t)
	dst := testWorld(t)
	defer func() {
		_ = src.Close()
		_ = dst.Close()
	}()

	spawn := Spawn{Position: mgl64.Vec3{2.5, 70, 2.5}, Rotation: cube.Rotation{-90, 0}}
	m := New(Config{Root: t.TempDir(), Log: discardLogger()})
	if _, err := m.RegisterWorld(Definition{Name: "overworld", Spawn: &spawn}, dst); err != nil {
		t.Fatalf("RegisterWorld() error = %v", err)
	}

	handle := dfworld.EntitySpawnOpts{Position: mgl64.Vec3{1, 64, 1}}.New(player.Type, player.Config{})
	runWorld(t, src, func(tx *dfworld.Tx) {
		tx.AddEntity(handle)
	})

	var travelErr error
	runWorld(t, src, func(tx *dfworld.Tx) {
		e, ok := handle.Entity(tx)
		if !ok {
			t.Fatal("player was not open in the source tx")
		}
		travelErr = m.TravelPlayerTx(tx, e.(*player.Player), "overworld")
	})

	if travelErr != nil {
		t.Fatalf("TravelPlayerTx() error = %v", travelErr)
	}

	inDestination := callEntity(t, handle, func(tx *dfworld.Tx, e dfworld.Entity) (bool, error) {
		p := e.(*player.Player)
		return tx.World() == dst && p.Position() == spawn.Position && p.Rotation() == spawn.Rotation, nil
	})
	if !inDestination {
		t.Fatal("player handle was not moved from the open transaction")
	}
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func testWorld(t *testing.T) *dfworld.World {
	t.Helper()
	return dfworld.Config{Log: discardLogger()}.New()
}

func runWorld(t *testing.T, w *dfworld.World, f func(*dfworld.Tx)) {
	t.Helper()
	if err := w.Do(f).Wait(context.Background()); err != nil {
		t.Fatalf("world task failed: %v", err)
	}
}

func callEntity[T any](t *testing.T, handle *dfworld.EntityHandle, f func(*dfworld.Tx, dfworld.Entity) (T, error)) T {
	t.Helper()
	result, err := dfworld.CallEntity(context.Background(), handle, f)
	if err != nil {
		t.Fatalf("entity call failed: %v", err)
	}
	return result
}

type testEntityConfig struct{}

func (testEntityConfig) Apply(*dfworld.EntityData) {}

type testEntityType struct{}

func (testEntityType) Open(_ *dfworld.Tx, handle *dfworld.EntityHandle, data *dfworld.EntityData) dfworld.Entity {
	return testEntity{handle: handle, data: data}
}

func (testEntityType) EncodeEntity() string { return "test:dfworlds_entity" }
func (testEntityType) BBox(dfworld.Entity) cube.BBox {
	return cube.Box(-0.25, 0, -0.25, 0.25, 0.5, 0.25)
}
func (testEntityType) DecodeNBT(map[string]any, *dfworld.EntityData) {}
func (testEntityType) EncodeNBT(*dfworld.EntityData) map[string]any  { return nil }

type testEntity struct {
	handle *dfworld.EntityHandle
	data   *dfworld.EntityData
}

func (e testEntity) H() *dfworld.EntityHandle { return e.handle }
func (e testEntity) Position() mgl64.Vec3     { return e.data.Pos }
func (e testEntity) Rotation() cube.Rotation  { return e.data.Rot }
func (e testEntity) Close() error             { return nil }
