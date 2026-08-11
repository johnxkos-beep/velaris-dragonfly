package dragonfly

import (
	"context"
	"log/slog"
	"sync"

	pmmpcompat "velaris-dragonfly/pmmpclient"

	"github.com/df-mc/dragonfly/server"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/player"
)

// RuntimeClient is the subset of *pmmpcompat.Client the Runtime needs. It
// exists so tests can swap in a fake client.
type RuntimeClient interface {
	PlayerJoin(ctx context.Context, uuid, name string) (pmmpcompat.PlayerJoinResult, []pmmpcompat.Action, error)
	PlayerJoinWithState(ctx context.Context, uuid, name string, state pmmpcompat.PlayerState, slots []pmmpcompat.InventorySlot) (pmmpcompat.PlayerJoinResult, []pmmpcompat.Action, error)
	PlayerQuit(ctx context.Context, uuid, name string) (pmmpcompat.PlayerQuitResult, []pmmpcompat.Action, error)
	Chat(ctx context.Context, uuid, name, message string) (pmmpcompat.ChatResult, []pmmpcompat.Action, error)
	Command(ctx context.Context, uuid, name, command string, args []string) (pmmpcompat.CommandResult, []pmmpcompat.Action, error)
	Commands(ctx context.Context) (pmmpcompat.CommandsResult, []pmmpcompat.Action, error)
	PlayerInventory(ctx context.Context, uuid string, slots []pmmpcompat.InventorySlot) (pmmpcompat.PlayerInventoryResult, []pmmpcompat.Action, error)
	FormResponse(ctx context.Context, uuid string, formID int, data any) (pmmpcompat.FormResponseResult, []pmmpcompat.Action, error)
	Tick(ctx context.Context, tick int) ([]pmmpcompat.Action, error)
}

// RuntimeOptions configures how bridge actions get applied back onto
// Dragonfly players (item mapping, health, flight, etc — see adapter.go).
type RuntimeOptions struct {
	Options
	Log *slog.Logger

	// IsOp, if set, is called on join with the player's XUID to decide
	// whether to tell PHP this player is a server operator (so plugin
	// permission checks like isOp() work correctly — e.g. an
	// admin-only "reset legendaries" button showing up). If nil,
	// every player is reported as a non-op.
	IsOp func(xuid string) bool
}

// Runtime is round 1 of the PMMP<->Dragonfly bridge: it wires player join,
// quit, chat, and command events into the PHP runtime process and applies
// whatever bridge actions come back (messages, forms, kicks, etc — see
// adapter.go for the full action set).
//
// NOT wired yet (round 2, once this compiles and joins/chat/commands work
// end-to-end): movement, block break/place, item use, entity damage,
// death, respawn, and inventory/state sync. Those all depended on a
// "dragonflyhost" snapshot helper library that isn't actually published
// anywhere, so they need to be written from scratch against the real
// Dragonfly API rather than blindly ported.
type Runtime struct {
	client RuntimeClient
	srv    *server.Server
	opts   RuntimeOptions

	mu      sync.Mutex
	players map[string]*player.Player
}

// NewRuntime creates a Runtime. Call RegisterPlayer for every player that
// joins, and Tick once per server tick (or on whatever interval you like —
// PHP-side scheduled tasks depend on tick count matching real time).
func NewRuntime(client RuntimeClient, srv *server.Server, opts RuntimeOptions) *Runtime {
	return &Runtime{
		client:  client,
		srv:     srv,
		opts:    opts,
		players: make(map[string]*player.Player),
	}
}

// Handler implements player.Handler and forwards events for one player
// into the PHP PMMP runtime.
type Handler struct {
	player.NopHandler

	rt *Runtime
	p  *player.Player
}

// RegisterPlayer tells the PHP runtime a player joined (firing PlayerJoinEvent
// and any plugin onJoin logic), applies whatever actions come back (e.g. a
// welcome message or join form), and returns a Handler to attach with
// p.Handle(...).
func (r *Runtime) RegisterPlayer(ctx context.Context, p *player.Player) (*Handler, error) {
	r.mu.Lock()
	r.players[p.UUID().String()] = p
	r.mu.Unlock()

	_, actions, err := r.client.PlayerJoin(ctx, p.UUID().String(), p.Name())
	if err != nil {
		return nil, err
	}
	r.applyActions(ctx, actions)

	if r.opts.IsOp != nil {
		isOp := r.opts.IsOp(p.XUID())
		state := pmmpcompat.PlayerState{IsOp: &isOp}
		if _, _, err := r.client.PlayerJoinWithState(ctx, p.UUID().String(), p.Name(), state, nil); err != nil {
			r.report(err)
		} else if r.opts.Log != nil {
			// Temporary diagnostic — this has silently not taken effect
			// with no error at all, so log exactly what Go computed and
			// sent. If this says is_op=true but in-game behavior still
			// says otherwise, the bug is entirely on the PHP side, not
			// here.
			r.opts.Log.Info("pmmp bridge: op state sync sent", "player", p.Name(), "xuid", p.XUID(), "uuid", p.UUID().String(), "is_op", isOp)
		}
	}

	return &Handler{rt: r, p: p}, nil
}

// Tick advances the PHP scheduler by one tick and applies any actions
// scheduled tasks emit (e.g. a plugin's periodic broadcast).
func (r *Runtime) Tick(ctx context.Context, tick int) error {
	actions, err := r.client.Tick(ctx, tick)
	if err != nil {
		return err
	}
	r.applyActions(ctx, actions)
	return nil
}

// SetFormMapper installs the FormMapper after construction. This exists
// because FormMapperFor needs a *Runtime to close over (so forms can
// call back into client.FormResponse), but the Runtime doesn't exist
// until after NewRuntime returns — so it can't be passed in as part of
// RuntimeOptions up front.
func (r *Runtime) SetFormMapper(mapper FormMapper) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.opts.Options.FormMapper = mapper
}

func (h *Handler) HandleQuit(p *player.Player) {
	ctx, cancel := h.rt.context()
	defer cancel()

	h.rt.mu.Lock()
	delete(h.rt.players, p.UUID().String())
	h.rt.mu.Unlock()

	_, actions, err := h.rt.client.PlayerQuit(ctx, p.UUID().String(), p.Name())
	if err != nil {
		h.rt.report(err)
		return
	}
	h.rt.applyActions(ctx, actions)
}

func (h *Handler) HandleChat(ctx *player.Context, message *string) {
	c, cancel := h.rt.context()
	defer cancel()

	result, actions, err := h.rt.client.Chat(c, h.p.UUID().String(), h.p.Name(), *message)
	if err != nil {
		h.rt.report(err)
		return
	}
	if result.Cancelled {
		ctx.Cancel()
		return
	}
	// A plugin may have rewritten the message (e.g. adding a rank prefix).
	if result.FormattedMessage != "" {
		*message = result.FormattedMessage
	} else if result.Message != "" {
		*message = result.Message
	}
	h.rt.applyActions(c, actions)
}

func (h *Handler) HandleCommandExecution(ctx *player.Context, command cmd.Command, args []string) {
	c, cancel := h.rt.context()
	defer cancel()

	stringArgs := make([]string, len(args))
	copy(stringArgs, args)

	// command.Name() — UNVERIFIED against your exact v0.11.1 cmd.Command
	// interface. If this doesn't compile, check what method actually
	// gives you the command's registered name (your own commands.go never
	// calls this, so there's no existing example to confirm against).
	_, actions, err := h.rt.client.Command(c, h.p.UUID().String(), h.p.Name(), command.Name(), stringArgs)
	if err != nil {
		h.rt.report(err)
		return
	}
	h.rt.applyActions(c, actions)
}

// applyActions applies a batch of bridge actions (messages, forms, kicks,
// broadcasts, etc) to the right Dragonfly targets. See adapter.go's
// PlayerTarget/ServerTarget for the full set of supported actions.
func (r *Runtime) applyActions(ctx context.Context, actions []pmmpcompat.Action) {
	if len(actions) == 0 {
		return
	}
	r.mu.Lock()
	resolver := NewResolver(r.srv, r.players, r.opts.Options)
	r.mu.Unlock()

	if err := pmmpcompat.ApplyActions(ctx, resolver, actions); err != nil {
		r.report(err)
	}
}

func (r *Runtime) context() (context.Context, context.CancelFunc) {
	return context.WithCancel(context.Background())
}

func (r *Runtime) report(err error) {
	if r.opts.Log != nil {
		r.opts.Log.Error("pmmp bridge error", "error", err.Error())
		return
	}
	println("pmmp bridge error:", err.Error())
}
