package dfworlds

import (
	"context"
	"errors"
	"fmt"

	"github.com/df-mc/dragonfly/server/player"
	dfworld "github.com/df-mc/dragonfly/server/world"
)

var ErrNilManager = errors.New("nil DFWorlds manager")

// Router provides named travel and a configurable default destination.
type Router struct {
	worlds             *Manager
	defaultDestination string
}

// NewRouter creates a Router around a Manager. The default destination name is
// validated but does not need to be loaded yet, which lets boot code create
// the router before opening every destination.
func NewRouter(worlds *Manager, defaultDestination string) (*Router, error) {
	if worlds == nil {
		return nil, ErrNilManager
	}
	clean, err := cleanName(defaultDestination)
	if err != nil {
		return nil, err
	}
	return &Router{worlds: worlds, defaultDestination: clean}, nil
}

// DefaultDestination returns the destination used by the default travel
// methods.
func (r *Router) DefaultDestination() string {
	if r == nil {
		return ""
	}
	return r.defaultDestination
}

// Worlds returns the Manager used by the router.
func (r *Router) Worlds() *Manager {
	if r == nil {
		return nil
	}
	return r.worlds
}

// Destinations returns all currently loaded destinations in stable order.
func (r *Router) Destinations() []LoadedWorld {
	if r == nil || r.worlds == nil {
		return nil
	}
	return r.worlds.Destinations()
}

// SendPlayerHandle sends a player handle to a destination by name. It blocks
// and must only be called from outside a world owner callback.
func (r *Router) SendPlayerHandle(ctx context.Context, handle *dfworld.EntityHandle, destination string, opts ...TravelOption) error {
	if r == nil || r.worlds == nil {
		return ErrNilManager
	}
	return r.worlds.TravelPlayerHandle(ctx, handle, destination, opts...)
}

// SendPlayerTx queues a player already opened in tx for a destination by name.
func (r *Router) SendPlayerTx(tx *dfworld.Tx, p *player.Player, destination string, opts ...TravelOption) error {
	if r == nil || r.worlds == nil {
		return ErrNilManager
	}
	return r.worlds.TravelPlayerTx(tx, p, destination, opts...)
}

// SendDefaultHandle sends a player handle to the configured default
// destination. It blocks and must only be called from outside a world owner
// callback.
func (r *Router) SendDefaultHandle(ctx context.Context, handle *dfworld.EntityHandle, opts ...TravelOption) error {
	if r == nil || r.worlds == nil {
		return ErrNilManager
	}
	if r.defaultDestination == "" {
		return fmt.Errorf("%w: default destination", ErrWorldNotFound)
	}
	return r.worlds.TravelPlayerHandle(ctx, handle, r.defaultDestination, opts...)
}

// SendDefaultTx queues a player already opened in tx for the configured
// default destination.
func (r *Router) SendDefaultTx(tx *dfworld.Tx, p *player.Player, opts ...TravelOption) error {
	if r == nil || r.worlds == nil {
		return ErrNilManager
	}
	if r.defaultDestination == "" {
		return fmt.Errorf("%w: default destination", ErrWorldNotFound)
	}
	return r.worlds.TravelPlayerTx(tx, p, r.defaultDestination, opts...)
}
