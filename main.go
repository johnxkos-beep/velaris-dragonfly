package main

import (
	"bufio"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/df-mc/dragonfly/server"
	"github.com/df-mc/dragonfly/server/cmd"
	dfentity "github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/player/chat"
	"github.com/df-mc/dragonfly/server/world"

	"velaris-dragonfly/bosses/demonking"
	"velaris-dragonfly/commands"
	"velaris-dragonfly/knockback"
	"velaris-dragonfly/legendary"
	"velaris-dragonfly/opsbans"
	"velaris-dragonfly/players"
	"velaris-dragonfly/pvp"
	"velaris-dragonfly/ranks"
	"velaris-dragonfly/rankforms"
	"velaris-dragonfly/restrict"
	"velaris-dragonfly/state"
)

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
	cmd.Register(cmd.New("spawndemonking", "Gives you a Demon King spawn egg.", nil, demonking.SpawnEggCommand{}))
	cmd.Register(cmd.New("legendary", "Opens the legendary weapon codex.", nil, legendary.Command{}))
	cmd.Register(cmd.New("pvp", "Toggles your PvP status or sets up a PvP zone.", nil, pvp.On{}, pvp.Off{}, pvp.Block{}))
	cmd.Register(cmd.New("restrict", "Gives a marker block that blocks non-ops from a 100x100 area.", nil, restrict.Command{}))
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

	// Serves the real Hoplite Weapons resource pack (attachables + 3D
	// geometry + hold animations) to every connecting client, on top of
	// Dragonfly's own auto-generated pack below. This is what actually
	// gives the legendary weapons their proper 3D held model instead of a
	// flat icon — Bedrock renders a custom item's real geometry the
	// instant a resource pack ships an "attachable" file whose identifier
	// matches the item's identifier (e.g. "bey:golem_hammer"), no
	// server-side model/animation logic needed at all. Since these items
	// were already registered under the exact same "bey:x" IDs the add-on
	// uses, this should "just work" the moment the pack is present.
	//
	// UNVERIFIED: "Resources.Folder" is my best-documented read of how
	// Dragonfly config points at an external resource-pack directory (this
	// wasn't confirmed against your exact Dragonfly version from this
	// environment — no network access to check the real UserConfig
	// struct). Drop hoplite_weapons_rp.mcpack (provided separately) into a
	// "resources" folder next to this binary on your server. If this field
	// name is wrong, `go build` will say so immediately and it's a
	// one-line fix — tell me the exact struct field it suggests instead.
	userConf.Resources.Folder = "resources"

	// Custom items must be registered with world.RegisterItem before
	// conf.New() below — that's when Dragonfly bakes its
	// auto-generated resource pack from whatever's currently
	// registered.
	if err := legendary.Init("legendary_claims.json"); err != nil {
		log.Error("failed to load legendary weapons", "error", err.Error())
	}
	demonking.Register()

	conf, err := userConf.Config(log)
	if err != nil {
		log.Error(err.Error())
		os.Exit(1)
	}

	// Add the Demon King boss AND the legendary weapon projectile entities
	// (Mjolnir's/Poseidon Trident's real thrown-weapon visuals) on top of
	// Dragonfly's default entity types (Entities defaults to
	// entity.DefaultRegistry if left unset, so this extends that default
	// set rather than replacing it).
	//
	// UNVERIFIED: assumes demonking.EntityRegistry()'s returned
	// world.EntityRegistry has a .Types() method to read back out what it
	// just built, so the projectile types can be appended on top rather
	// than lost — matches how demonking.EntityRegistry() itself builds off
	// dfentity.DefaultRegistry.Types() internally. If .Types() doesn't
	// exist on world.EntityRegistry, the compiler error will say so and
	// it's a quick fix (worst case, copy demonking.EntityRegistry()'s own
	// one-line body here and add legendary.ProjectileTypes()... to its
	// slice directly instead of going through this indirection).
	entityTypes := append(demonking.EntityRegistry().Types(), legendary.ProjectileTypes()...)
	entityTypes = append(entityTypes, legendary.HUDTypes()...)
	entityTypes = append(entityTypes, legendary.EagleTypes()...)
	conf.Entities = dfentity.DefaultRegistry.Config().New(entityTypes)

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
	pvp.Cfg, err = pvp.Load("pvp.json")
	if err != nil {
		log.Error("failed to load pvp config", "error", err.Error())
		os.Exit(1)
	}
	restrict.Cfg, err = restrict.Load("restrict.json")
	if err != nil {
		log.Error("failed to load restrict config", "error", err.Error())
		os.Exit(1)
	}

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
		p.Handle(players.NewPlayerHandler(p, state.Ranks, state.RankDefs, log))
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
