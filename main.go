package main

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/df-mc/dragonfly/server"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/player/chat"
	"github.com/df-mc/dragonfly/server/world"

	"velaris-dragonfly/commands"
	"velaris-dragonfly/knockback"
	"velaris-dragonfly/opsbans"
	"velaris-dragonfly/players"
	pmmpbridge "velaris-dragonfly/pmmpbridge"
	pmmpcompat "velaris-dragonfly/pmmpclient"
	"velaris-dragonfly/ranks"
	"velaris-dragonfly/rankforms"
	"velaris-dragonfly/state"
	"velaris-dragonfly/worldgen"
)

// worldSeed controls the terrain/biome layout of the overworld and nether.
// Change this to regenerate a different-looking world.
const worldSeed = 1337

// registerCommands registers every command. Call once before the server
// starts accepting players.
func registerCommands() {
	cmd.Register(cmd.New("ping", "Replies with Pong.", nil, commands.Ping{}))
	cmd.Register(cmd.New("gamemode", "Changes your game mode.", []string{"gm"}, commands.GameMode{}))
	cmd.Register(cmd.New("gms", "Switches you to survival mode.", nil, commands.Gms{}))
	cmd.Register(cmd.New("gmc", "Switches you to creative mode.", nil, commands.Gmc{}))
	cmd.Register(cmd.New("gma", "Switches you to adventure mode.", nil, commands.Gma{}))
	cmd.Register(cmd.New("gmsp", "Switches you to spectator mode.", nil, commands.Gmsp{}))
	cmd.Register(cmd.New("tp", "Teleports you to a set of coordinates.", []string{"teleport"}, commands.Tp{}))
	cmd.Register(cmd.New("feed", "Restores your hunger.", nil, commands.Feed{}))
	cmd.Register(cmd.New("coords", "Toggles the on-screen coordinate display.", []string{"xyz"}, commands.Coords{}))
	cmd.Register(cmd.New("setworldspawn", "Sets the world spawn point.", nil, commands.SetWorldSpawn{}))
	cmd.Register(cmd.New("time", "Changes or queries the world time.", nil, commands.TimeSet{}, commands.TimeQuery{}))
	cmd.Register(cmd.New("weather", "Changes the world weather.", nil, commands.Weather{}))
	cmd.Register(cmd.New("op", "Grants a player operator status.", nil, commands.Op{}))
	cmd.Register(cmd.New("deop", "Revokes a player's operator status.", nil, commands.Deop{}))
	cmd.Register(cmd.New("kick", "Disconnects a player from the server.", nil, commands.Kick{}))
	cmd.Register(cmd.New("ban", "Bans a player from the server.", nil, commands.Ban{}))
	cmd.Register(cmd.New("rank", "Opens the rank management menu.", nil, rankforms.RankMenu{}))
	cmd.Register(cmd.New("kb", "Opens the KB configuration editor.", nil, knockback.Command{}))
}

// ---------------------------------------------------------------------
// Console command handling
// ---------------------------------------------------------------------
//
// Console commands are handled directly here rather than routed through
// the full in-game cmd.Runnable system, since that requires a *world.Tx
// which isn't obtainable from outside a player-triggered event. Console is
// always a trusted source anyway, so this keeps things simple: op, deop,
// and kick all work from the Pterodactyl console box. Anything that needs
// to touch player/world state (like kick's Disconnect call) goes through
// state.FindAndActOnline so it runs inside a proper transaction — see its
// doc comment for why that matters. Ban is in-game only for now (see
// commands.Ban.Run).

// runConsole reads lines from stdin in a loop and handles admin commands
// typed into the Pterodactyl console. Call this in its own goroutine from
// main() after srv.Listen().
func runConsole(srv *server.Server) {
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		func() {
			// Guard against any panic in a single console command taking
			// down the whole server process — log it and keep going. Note
			// this only catches panics on THIS goroutine; anything that
			// mutates player/world state must go through
			// state.FindAndActOnline (see its doc comment) or a panic there
			// will happen on dragonfly's internal tick goroutine instead,
			// where this recover can't reach it.
			defer func() {
				if r := recover(); r != nil {
					fmt.Println("Console command panicked (recovered):", r)
				}
			}()

			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				return
			}
			fields := strings.Fields(line)
			name := strings.ToLower(fields[0])
			args := fields[1:]

			switch name {
			case "op":
				target, _ := state.MatchOnlineGreedy(args)
				if target == nil {
					fmt.Println("No online player matches that name.")
					return
				}
				if err := state.Ops.SetOp(target.XUID(), true); err != nil {
					fmt.Println("Failed to save op status:", err)
					return
				}
				target.Message("§aYou are now a server operator.")
				fmt.Printf("Made %s a server operator.\n", target.Name())

			case "deop":
				target, _ := state.MatchOnlineGreedy(args)
				if target == nil {
					fmt.Println("No online player matches that name.")
					return
				}
				if err := state.Ops.SetOp(target.XUID(), false); err != nil {
					fmt.Println("Failed to save op status:", err)
					return
				}
				target.Message("§cYou are no longer a server operator.")
				fmt.Printf("Removed %s as a server operator.\n", target.Name())

			case "kick":
				reason := "Kicked by an operator."
				found := state.FindAndActOnline(srv, args, func(target *player.Player, rest []string) {
					if len(rest) > 0 {
						reason = strings.Join(rest, " ")
					}
					target.Disconnect(reason)
					fmt.Printf("Kicked %s: %s\n", target.Name(), reason)
				})
				if !found {
					fmt.Println("No online player matches that name.")
				}

			default:
				fmt.Println("Unknown console command:", name, "(available: op, deop, kick — use /ban in-game instead, it's an op-restricted command)")
			}
		}()
	}
}

// ---------------------------------------------------------------------
// PMMP bridge wiring
// ---------------------------------------------------------------------
//
// Dragonfly only lets a player have ONE Handler at a time — calling
// p.Handle() twice replaces the first handler rather than adding to it.
// combinedHandler exists solely to fix that: it embeds our existing
// players.PlayerHandler (knockback, ranks, autosmelt, etc — untouched)
// and, on top of that, explicitly forwards quit/chat/command events to
// the PMMP bridge handler too, when the bridge is running. Every other
// event (hurt, attack, item use, death, block place) still goes through
// players.PlayerHandler exactly as before.
type combinedHandler struct {
	*players.PlayerHandler
	bridge *pmmpbridge.Handler // nil if the PHP bridge failed to start
}

func (h *combinedHandler) HandleQuit(p *player.Player) {
	h.PlayerHandler.HandleQuit(p)
	if h.bridge != nil {
		h.bridge.HandleQuit(p)
	}
}

func (h *combinedHandler) HandleChat(ctx *player.Context, message *string) {
	h.PlayerHandler.HandleChat(ctx, message)
	if h.bridge != nil {
		h.bridge.HandleChat(ctx, message)
	}
}

func (h *combinedHandler) HandleCommandExecution(ctx *player.Context, command cmd.Command, args []string) {
	// players.PlayerHandler doesn't handle commands itself (it relies on
	// player.NopHandler's default here), so this only needs to reach the
	// bridge. NOTE: this only fires for commands already registered with
	// Dragonfly's cmd.Register — a PMMP plugin's own commands (e.g.
	// /kit from HopliteLegendary) won't reach here yet. Registering
	// those dynamically from PHP's command list is round 2.
	if h.bridge != nil {
		h.bridge.HandleCommandExecution(ctx, command, args)
	}
}

// startPMMPBridge launches the PHP PMMP compatibility runtime and loads
// plugins from the given folder. If anything fails here, it logs the
// error and returns nil — the server keeps running fine without the
// bridge, just without PMMP plugins active.
func startPMMPBridge(ctx context.Context, srv *server.Server, log *slog.Logger) *pmmpbridge.Runtime {
	client, err := pmmpcompat.StartWithArgs(ctx, "php-bin/php", nil, "pmmpcompat/bin/pmmpcompat-runtime.php", "plugins")
	if err != nil {
		log.Error("pmmp bridge: failed to start PHP runtime", "error", err.Error())
		return nil
	}

	if _, _, err := client.Load(ctx); err != nil {
		log.Error("pmmp bridge: failed to load plugins", "error", err.Error())
		if out, rerr := client.Stderr(); rerr == nil && len(out) > 0 {
			log.Error("pmmp bridge: php stderr", "output", string(out))
		}
		return nil
	}
	if _, err := client.Enable(ctx); err != nil {
		log.Error("pmmp bridge: failed to enable plugins", "error", err.Error())
		return nil
	}

	log.Info("pmmp bridge: PHP runtime started and plugins loaded")
	rt := pmmpbridge.NewRuntime(client, srv, pmmpbridge.RuntimeOptions{
		Options: pmmpbridge.Options{
			ItemMapper:   pmmpbridge.DefaultItemMapper,
			HealthSetter: pmmpbridge.EventedHealthSetter,
			// FormMapper, AllowFlightSetter, ViewDistanceSetter left
			// unset for round 1 — plugins that send forms or touch
			// flight/view-distance will get a logged error via
			// ErrMissingMapper rather than crashing anything.
		},
		Log: log,
	})
	rt.SetFormMapper(pmmpbridge.FormMapperFor(rt))

	// Round 2: ask PHP what commands its plugins registered (e.g.
	// HopliteLegendary's /kit) and wire each one up as a real Dragonfly
	// command. Must happen after Load/Enable — commands can't be
	// registered before the plugins defining them have loaded.
	if err := pmmpbridge.RegisterCommands(ctx, client, rt, log); err != nil {
		log.Error("pmmp bridge: failed to register commands", "error", err.Error())
	}

	return rt
}

// ---------------------------------------------------------------------
// main
// ---------------------------------------------------------------------

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	slog.SetDefault(log)

	// Use Pterodactyl's SERVER_PORT env var if set, so the listen address
	// always matches whatever allocation is assigned in the panel.
	port := os.Getenv("SERVER_PORT")
	if port == "" {
		port = "19132"
	}

	userConf := server.DefaultConfig()
	userConf.Network.Address = ":" + port
	userConf.Server.Name = "Velaris DragonFly"

	conf, err := userConf.Config(log)
	if err != nil {
		log.Error(err.Error())
		os.Exit(1)
	}

	// Use our custom noise-based generator for the overworld and nether
	// instead of Dragonfly's default flat generator.
	conf.Generator = func(dim world.Dimension) world.Generator {
		if dim == world.Nether {
			return worldgen.NewNether(worldSeed)
		}
		return worldgen.NewOverworld(worldSeed)
	}

	conf.Allower = nil
	srv := conf.New()
	srv.CloseOnProgramEnd()
	state.Server = srv

	chat.Global.Subscribe(chat.StdoutSubscriber{})

	registerCommands()

	state.Ranks, err = ranks.LoadRanks("ranks.json")
	if err != nil {
		log.Error("failed to load ranks", "error", err.Error())
		os.Exit(1)
	}
	state.RankDefs, err = ranks.LoadRankDefs("rank_defs.json")
	if err != nil {
		log.Error("failed to load rank definitions", "error", err.Error())
		os.Exit(1)
	}
	state.Ops, err = opsbans.LoadOps("ops.json")
	if err != nil {
		log.Error("failed to load ops", "error", err.Error())
		os.Exit(1)
	}
	state.Bans, err = opsbans.LoadBans("bans.json")
	if err != nil {
		log.Error("failed to load bans", "error", err.Error())
		os.Exit(1)
	}
	knockback.Cfg, err = knockback.Load("kb.json")
	if err != nil {
		log.Error("failed to load kb config", "error", err.Error())
		os.Exit(1)
	}

	// Start the PMMP compatibility bridge (PHP process + your plugins).
	// bridgeCtx is intentionally not tied to a timeout — the PHP process
	// should live as long as the server does. Cancelling it on shutdown
	// is handled by srv.CloseOnProgramEnd() triggering program exit,
	// which kills the child process along with it.
	bridgeCtx := context.Background()
	bridge := startPMMPBridge(bridgeCtx, srv, log)

	srv.Listen()
	go runConsole(srv)

	for p := range srv.Accept() {
		// Check the ban list before letting the player do anything else.
		if reason, banned := state.Bans.Reason(p.XUID()); banned {
			p.Disconnect(reason)
			continue
		}

		// Auto-op the very first player to ever join, so there's always a
		// way to grant op in-game even with an empty ops.json.
		if state.Ops.Empty() {
			_ = state.Ops.SetOp(p.XUID(), true)
			p.Message("§aYou've been made a server operator (first join).")
		}

		state.TrackJoin(p)
		handler := &combinedHandler{PlayerHandler: players.NewPlayerHandler(p, state.Ranks, state.RankDefs, log)}
		if bridge != nil {
			if bh, err := bridge.RegisterPlayer(bridgeCtx, p); err != nil {
				log.Error("pmmp bridge: failed to register player", "player", p.Name(), "error", err.Error())
			} else {
				handler.bridge = bh
			}
		}
		p.Handle(handler)
		ranks.ApplyNameTag(p, state.Ranks, state.RankDefs)

		// Basic server defaults on join: survival mode + coordinates shown.
		p.SetGameMode(world.GameModeSurvival)
		p.ShowCoordinates()
		state.CoordsMu.Lock()
		state.CoordsState[p.XUID()] = true
		state.CoordsMu.Unlock()

		p.Message("§aWelcome to Velaris DragonFly!")
	}
}
