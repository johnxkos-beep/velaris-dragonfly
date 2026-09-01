package dfworlds

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync/atomic"

	"github.com/df-mc/dragonfly/server/player"
	dfworld "github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

var (
	// ErrEntityClosed is returned when the entity handle can no longer be
	// opened in a world transaction.
	ErrEntityClosed = errors.New("entity handle is closed")
	// ErrNotPlayer is returned when a player-specific transfer receives a
	// handle for a non-player entity.
	ErrNotPlayer = errors.New("entity is not a player")
)

// TransferHandle moves an entity handle to a loaded world. The entity keeps its
// current position. It blocks and must only be called from outside a world
// owner callback.
func (m *Manager) TransferHandle(ctx context.Context, handle *dfworld.EntityHandle, worldName string) error {
	w, err := m.MustWorld(worldName)
	if err != nil {
		return err
	}
	return TransferHandle(ctx, handle, w)
}

// TransferPlayerHandle moves a player handle to a loaded world and teleports it
// to pos after attach. It blocks and must only be called from outside a world
// owner callback.
func (m *Manager) TransferPlayerHandle(ctx context.Context, handle *dfworld.EntityHandle, worldName string, pos mgl64.Vec3) error {
	w, err := m.MustWorld(worldName)
	if err != nil {
		return err
	}
	return TransferPlayerHandle(ctx, handle, w, pos)
}

// TransferPlayerTx queues a player already opened in tx for a loaded world and
// teleports them to pos after the player is attached to the destination world.
func (m *Manager) TransferPlayerTx(tx *dfworld.Tx, p *player.Player, worldName string, pos mgl64.Vec3) error {
	w, err := m.MustWorld(worldName)
	if err != nil {
		return err
	}
	return transferFromTx(tx, p, w, nil, func(_ *dfworld.Tx, p *player.Player) error {
		p.Teleport(pos)
		return nil
	})
}

// TravelPlayerHandle moves a player handle to a loaded destination and applies
// the destination's spawn, rotation, and default game mode. It blocks and must
// only be called from outside a world owner callback.
func (m *Manager) TravelPlayerHandle(ctx context.Context, handle *dfworld.EntityHandle, worldName string, opts ...TravelOption) error {
	dest, err := m.MustDestination(worldName)
	if err != nil {
		return err
	}
	options := defaultTravelOptions(dest)
	for _, opt := range opts {
		opt(&options)
	}
	return transferPlayerOffOwner(ctx, handle, dest.World, func(tx *dfworld.Tx, p *player.Player) error {
		if options.Before != nil {
			return options.Before(tx, p)
		}
		return nil
	}, func(tx *dfworld.Tx, p *player.Player) error {
		if options.GameMode != nil {
			p.SetGameMode(options.GameMode)
		}
		if options.Spawn != nil {
			applySpawn(p, *options.Spawn)
		}
		if options.After != nil {
			return options.After(tx, p)
		}
		return nil
	})
}

// TravelPlayerTx queues a player already opened in tx for a loaded destination
// and applies the destination's spawn, rotation, and default game mode. Use
// this from Dragonfly commands and event handlers that receive a world.Tx. A
// cross-world destination attach completes after the current transaction.
func (m *Manager) TravelPlayerTx(tx *dfworld.Tx, p *player.Player, worldName string, opts ...TravelOption) error {
	dest, err := m.MustDestination(worldName)
	if err != nil {
		return err
	}
	options := defaultTravelOptions(dest)
	for _, opt := range opts {
		opt(&options)
	}
	return transferFromTx(tx, p, dest.World, func(tx *dfworld.Tx, p *player.Player) error {
		if options.Before != nil {
			return options.Before(tx, p)
		}
		return nil
	}, func(tx *dfworld.Tx, p *player.Player) error {
		if options.GameMode != nil {
			p.SetGameMode(options.GameMode)
		}
		if options.Spawn != nil {
			applySpawn(p, *options.Spawn)
		}
		if options.After != nil {
			return options.After(tx, p)
		}
		return nil
	})
}

// TravelHook runs during player travel. Before hooks run in the source world
// transaction, while After hooks run in the destination world transaction.
type TravelHook func(tx *dfworld.Tx, p *player.Player) error

// TravelOptions controls a single player travel operation.
type TravelOptions struct {
	Spawn    *Spawn
	GameMode dfworld.GameMode
	Before   TravelHook
	After    TravelHook
}

// TravelOption mutates TravelOptions.
type TravelOption func(*TravelOptions)

// WithSpawn overrides the destination's configured spawn for one travel.
func WithSpawn(spawn Spawn) TravelOption {
	return func(o *TravelOptions) {
		o.Spawn = &spawn
	}
}

// WithGameMode overrides the destination's configured game mode for one
// travel.
func WithGameMode(mode dfworld.GameMode) TravelOption {
	return func(o *TravelOptions) {
		o.GameMode = mode
	}
}

// BeforeTravel adds a hook that runs before the player leaves their current
// world.
func BeforeTravel(h TravelHook) TravelOption {
	return func(o *TravelOptions) {
		o.Before = chainTravelHooks(o.Before, h)
	}
}

// AfterTravel adds a hook that runs after the player is attached to the
// destination world.
func AfterTravel(h TravelHook) TravelOption {
	return func(o *TravelOptions) {
		o.After = chainTravelHooks(o.After, h)
	}
}

// TransferHandle moves an entity handle to target. The entity keeps its current
// position. It blocks and must only be called from outside a world owner
// callback.
func TransferHandle(ctx context.Context, handle *dfworld.EntityHandle, target *dfworld.World) error {
	return transferOffOwner[dfworld.Entity](ctx, handle, target, nil, nil)
}

// TransferPlayerHandle moves a player handle to target and teleports it to pos
// after attach. It blocks and must only be called from outside a world owner
// callback.
func TransferPlayerHandle(ctx context.Context, handle *dfworld.EntityHandle, target *dfworld.World, pos mgl64.Vec3) error {
	return transferPlayerOffOwner(ctx, handle, target, nil, func(_ *dfworld.Tx, p *player.Player) error {
		p.Teleport(pos)
		return nil
	})
}

// TransferPlayerTx queues a player already opened in tx for target and
// teleports them to pos after attach.
func TransferPlayerTx(tx *dfworld.Tx, p *player.Player, target *dfworld.World, pos mgl64.Vec3) error {
	return transferFromTx(tx, p, target, nil, func(_ *dfworld.Tx, p *player.Player) error {
		p.Teleport(pos)
		return nil
	})
}

func transferOffOwner[E dfworld.Entity](
	ctx context.Context,
	handle *dfworld.EntityHandle,
	target *dfworld.World,
	before func(*dfworld.Tx, E) error,
	after func(*dfworld.Tx, E) error,
) error {
	if handle == nil {
		return fmt.Errorf("%w: <nil>", ErrEntityClosed)
	}
	if target == nil {
		return errors.New("target world is nil")
	}

	result, err := dfworld.CallRef(ctx, dfworld.NewEntityRef[E](handle), func(tx *dfworld.Tx, e E) (transferSource, error) {
		if before != nil {
			if err := before(tx, e); err != nil {
				return transferSource{}, err
			}
		}
		if tx.World() == target {
			if after != nil {
				return transferSource{same: true}, after(tx, e)
			}
			return transferSource{same: true}, nil
		}
		if removed := tx.RemoveEntity(e); removed == nil {
			return transferSource{}, errors.New("entity is not present in its current world")
		}
		return transferSource{world: tx.World()}, nil
	})
	if err != nil {
		if errors.Is(err, dfworld.ErrEntityClosed) {
			return ErrEntityClosed
		}
		return err
	}
	if result.same {
		return nil
	}

	attached := false
	_, err = dfworld.Call(ctx, target, func(tx *dfworld.Tx) (struct{}, error) {
		e := tx.AddEntity(handle)
		attached = true
		value, ok := e.(E)
		if !ok {
			return struct{}{}, fmt.Errorf("%w: got %T", dfworld.ErrEntityType, e)
		}
		if after != nil {
			return struct{}{}, after(tx, value)
		}
		return struct{}{}, nil
	})
	if err == nil || attached {
		return err
	}
	restoreErr := restoreHandle(result.world, handle)
	return errors.Join(err, restoreErr)
}

func transferPlayerOffOwner(
	ctx context.Context,
	handle *dfworld.EntityHandle,
	target *dfworld.World,
	before TravelHook,
	after TravelHook,
) error {
	err := transferOffOwner[*player.Player](ctx, handle, target, before, after)
	if errors.Is(err, dfworld.ErrEntityType) {
		return fmt.Errorf("%w: %v", ErrNotPlayer, err)
	}
	return err
}

func transferFromTx(
	tx *dfworld.Tx,
	p *player.Player,
	target *dfworld.World,
	before TravelHook,
	after TravelHook,
) error {
	if tx == nil {
		return errors.New("source tx is nil")
	}
	if p == nil {
		return fmt.Errorf("%w: <nil>", ErrNotPlayer)
	}
	if target == nil {
		return errors.New("target world is nil")
	}
	if before != nil {
		if err := before(tx, p); err != nil {
			return err
		}
	}
	if tx.World() == target {
		if after != nil {
			return after(tx, p)
		}
		return nil
	}
	handle := tx.RemoveEntity(p)
	if handle == nil {
		return errors.New("entity is not present in its current world")
	}

	source := tx.World()
	var attached atomic.Bool
	var afterErr error
	task := target.Do(func(tx *dfworld.Tx) {
		e := tx.AddEntity(handle)
		attached.Store(true)
		p, ok := e.(*player.Player)
		if !ok {
			afterErr = fmt.Errorf("%w: %T", ErrNotPlayer, e)
			return
		}
		if after != nil {
			afterErr = after(tx, p)
		}
	})
	select {
	case <-task.Done():
		if err := task.Err(); err != nil && !attached.Load() {
			tx.AddEntity(handle)
			return err
		}
		return errors.Join(task.Err(), afterErr)
	default:
		task.OnDone(func(err error) {
			if err != nil && !attached.Load() {
				restoreHandleAsync(source, handle)
				return
			}
			if afterErr != nil {
				slog.Error("dfworlds: destination travel hook failed", "error", afterErr)
			}
		})
		return nil
	}
}

type transferSource struct {
	world *dfworld.World
	same  bool
}

func chainTravelHooks(first, second TravelHook) TravelHook {
	if first == nil {
		return second
	}
	if second == nil {
		return first
	}
	return func(tx *dfworld.Tx, p *player.Player) error {
		if err := first(tx, p); err != nil {
			return err
		}
		return second(tx, p)
	}
}

func restoreHandle(source *dfworld.World, handle *dfworld.EntityHandle) error {
	if source == nil {
		_ = handle.Close()
		return errors.New("restore entity: source world is nil")
	}
	_, err := dfworld.Call(context.Background(), source, func(tx *dfworld.Tx) (struct{}, error) {
		tx.AddEntity(handle)
		return struct{}{}, nil
	})
	if err != nil {
		_ = handle.Close()
		return fmt.Errorf("restore entity to source world: %w", err)
	}
	return nil
}

func restoreHandleAsync(source *dfworld.World, handle *dfworld.EntityHandle) {
	if source == nil {
		_ = handle.Close()
		return
	}
	source.Do(func(tx *dfworld.Tx) {
		tx.AddEntity(handle)
	}).OnDone(func(err error) {
		if err != nil {
			_ = handle.Close()
		}
	})
}

func defaultTravelOptions(dest LoadedWorld) TravelOptions {
	spawn := dest.Spawn
	return TravelOptions{
		Spawn:    &spawn,
		GameMode: dest.GameMode,
	}
}

func applySpawn(p *player.Player, spawn Spawn) {
	p.Teleport(spawn.Position)

	current := p.Rotation()
	deltaYaw := spawn.Rotation.Yaw() - current.Yaw()
	deltaPitch := spawn.Rotation.Pitch() - current.Pitch()
	p.Move(mgl64.Vec3{}, deltaYaw, deltaPitch)
}
