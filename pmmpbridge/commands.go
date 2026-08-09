package dragonfly

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	pmmpcompat "velaris-dragonfly/pmmpclient"

	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
)

// RegisterCommands asks the PHP runtime for every command its loaded
// plugins registered (e.g. HopliteLegendary's /kit) and wires each one
// up as a real Dragonfly command that, when run, forwards the raw text
// to PHP and applies whatever actions come back.
//
// Call this once, after client.Load/Enable have already succeeded (see
// startPMMPBridge in main.go) — commands can't be registered before the
// plugins that define them have loaded.
func RegisterCommands(ctx context.Context, client RuntimeClient, rt *Runtime, log *slog.Logger) error {
	result, _, err := client.Commands(ctx)
	if err != nil {
		return err
	}
	registered := 0
	for _, info := range result.Commands {
		if registerOneCommand(rt, info, log) {
			registered++
		}
	}
	if log != nil {
		log.Info("pmmp bridge: registered commands", "count", registered, "of", len(result.Commands))
	}
	return nil
}

// registerOneCommand registers a single PMMP command. It recovers from
// any panic (e.g. a name collision with an existing Dragonfly command)
// so one bad command can't take the rest of registration — or the
// server — down with it, and logs+skips instead.
func registerOneCommand(rt *Runtime, info pmmpcompat.CommandInfo, log *slog.Logger) (ok bool) {
	defer func() {
		if r := recover(); r != nil {
			ok = false
			if log != nil {
				log.Error("pmmp bridge: failed to register command", "command", info.Name, "error", fmt.Sprintf("%v", r))
			}
		}
	}()

	name := strings.ToLower(strings.TrimSpace(info.Name))
	if name == "" {
		return false
	}

	aliases := make([]string, 0, len(info.Aliases))
	seen := map[string]bool{name: true}
	for _, alias := range info.Aliases {
		alias = strings.ToLower(strings.TrimSpace(alias))
		if alias == "" || seen[alias] {
			continue
		}
		seen[alias] = true
		aliases = append(aliases, alias)
	}

	description := info.Description
	if description == "" {
		description = "PocketMine plugin command"
	}

	cmd.Register(cmd.New(name, description, aliases, pmmpCommand{rt: rt, label: name}))
	return true
}

// pmmpCommand is a generic Dragonfly command that forwards everything
// typed after the command name to the PHP PMMP runtime as raw text,
// splits on whitespace. PMMP plugins already do their own argument
// parsing on the PHP side, so this deliberately doesn't try to model
// each command's specific typed parameters in Go — that would mean
// writing a different struct by hand for every single plugin command,
// which doesn't scale and can't be generated at registration time the
// way this needs to be. This means Dragonfly's client-side command UI
// won't show fancy per-argument autocomplete for these — everything
// after the command name is just one free-typed field.
type pmmpCommand struct {
	rt    *Runtime `cmd:"-"`
	label string   `cmd:"-"`

	Args cmd.Optional[string] `cmd:"args"`
}

func (c pmmpCommand) Run(src cmd.Source, o *cmd.Output, tx *world.Tx) {
	p, ok := src.(*player.Player)
	if !ok {
		o.Print("PocketMine plugin commands can only be run by players.")
		return
	}

	var rawArgs []string
	if text, ok := c.Args.Load(); ok {
		rawArgs = strings.Fields(text)
	}

	callCtx, cancel := c.rt.context()
	defer cancel()

	// PHP's view of the player's inventory only exists because we push
	// it there — Dragonfly never tells PHP about it on its own. Plugin
	// commands that check "do you have the materials for this recipe"
	// (like HopliteLegendary's crafting) read PHP's copy, so it has to
	// be fresh right before the command runs, not just once at join.
	if slots := inventorySlotsFor(p); slots != nil {
		if _, _, err := c.rt.client.PlayerInventory(callCtx, p.UUID().String(), slots); err != nil {
			c.rt.report(err)
			// Not fatal — the command can still run, just against a
			// possibly-stale inventory view. Better than not running
			// the command at all.
		}
	}

	_, actions, err := c.rt.client.Command(callCtx, p.UUID().String(), p.Name(), c.label, rawArgs)
	if err != nil {
		o.Printf("PocketMine command failed: %v", err)
		c.rt.report(err)
		return
	}
	c.rt.applyActions(callCtx, actions)
}

// inventorySlotsFor reads a player's real Dragonfly inventory and
// converts it to the slot format PHP expects. Contents() only returns
// non-empty stacks (no slot index attached), so slots are numbered
// sequentially here — that's fine, since PHP only uses this to check
// whether materials are present, not to mirror the exact layout.
func inventorySlotsFor(p *player.Player) []pmmpcompat.InventorySlot {
	// Neither .All() nor .Contents() exist on this Dragonfly version —
	// switching to the one access pattern documented consistently
	// across every version of this package: iterate slots individually
	// via Size()/Item(slot), matching how the inventory package's own
	// error messages describe valid access.
	inv := p.Inventory()
	size := inv.Size()
	slots := make([]pmmpcompat.InventorySlot, 0, size)
	for i := 0; i < size; i++ {
		st, err := inv.Item(i)
		if err != nil || st.Empty() {
			continue
		}
		id, _ := st.Item().EncodeItem()
		slots = append(slots, pmmpcompat.InventorySlot{
			Slot: i,
			Item: pmmpcompat.InventoryItem{
				TypeID: id,
				Name:   id,
				Count:  st.Count(),
			},
		})
	}
	return slots
}
