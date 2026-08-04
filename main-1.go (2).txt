package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/df-mc/dragonfly/server"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/player/chat"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// ---------------------------------------------------------------------
// Ranks
// ---------------------------------------------------------------------

// Rank represents a single permission tier.
type Rank struct {
	Name string
	Tag  string // Chat prefix shown before the player's name.
}

var (
	Owner   = Rank{Name: "Owner", Tag: "§4[Owner]§r"}
	Admin   = Rank{Name: "Admin", Tag: "§c[Admin]§r"}
	YouTube = Rank{Name: "YouTube", Tag: "§c[YT]§r"}
	Default = Rank{Name: "Default", Tag: "§7[Member]§r"}
)

func (r Rank) ChatTag() string { return r.Tag }

var ranksByName = map[string]Rank{
	Owner.Name:   Owner,
	Admin.Name:   Admin,
	YouTube.Name: YouTube,
	Default.Name: Default,
}

// RankSet holds the XUID -> rank-name mapping for the whole server, and
// knows how to persist itself to disk.
type RankSet struct {
	mu   sync.RWMutex
	path string
	data map[string]string // xuid -> rank name
}

// LoadRanks reads the rank set from the JSON file at path. If the file does
// not exist, an empty (but valid) RankSet is returned instead of an error.
func LoadRanks(path string) (*RankSet, error) {
	s := &RankSet{path: path, data: map[string]string{}}

	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return s, nil
	} else if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &s.data); err != nil {
		return nil, err
	}
	return s, nil
}

// Of returns the Rank for the given XUID, falling back to Default if the
// player has no rank on file.
func (s *RankSet) Of(xuid string) Rank {
	s.mu.RLock()
	defer s.mu.RUnlock()

	name, ok := s.data[xuid]
	if !ok {
		return Default
	}
	if r, ok := ranksByName[name]; ok {
		return r
	}
	return Default
}

// SetRank assigns a rank to the given XUID and persists the change to disk.
func (s *RankSet) SetRank(xuid string, r Rank) error {
	s.mu.Lock()
	s.data[xuid] = r.Name
	b, err := json.MarshalIndent(s.data, "", "  ")
	s.mu.Unlock()

	if err != nil {
		return err
	}
	return os.WriteFile(s.path, b, 0644)
}

// ---------------------------------------------------------------------
// Ops
// ---------------------------------------------------------------------

// OpSet tracks which XUIDs are server operators, persisted to ops.json.
type OpSet struct {
	mu   sync.RWMutex
	path string
	data map[string]bool
}

// LoadOps reads the op set from path. If the file doesn't exist, an empty
// set is returned.
func LoadOps(path string) (*OpSet, error) {
	s := &OpSet{path: path, data: map[string]bool{}}

	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return s, nil
	} else if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &s.data); err != nil {
		return nil, err
	}
	return s, nil
}

// IsOp reports whether the given XUID is a server operator.
func (s *OpSet) IsOp(xuid string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.data[xuid]
}

// Empty reports whether there are no operators on file yet. Used to
// auto-op the first player who ever joins, so there's always at least one
// way to grant op in-game.
func (s *OpSet) Empty() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.data) == 0
}

// SetOp adds or removes op status for the given XUID and persists it.
func (s *OpSet) SetOp(xuid string, op bool) error {
	s.mu.Lock()
	if op {
		s.data[xuid] = true
	} else {
		delete(s.data, xuid)
	}
	b, err := json.MarshalIndent(s.data, "", "  ")
	s.mu.Unlock()

	if err != nil {
		return err
	}
	return os.WriteFile(s.path, b, 0644)
}

// ---------------------------------------------------------------------
// Bans
// ---------------------------------------------------------------------

// BanSet tracks banned XUIDs and their reasons, persisted to bans.json.
type BanSet struct {
	mu   sync.RWMutex
	path string
	data map[string]string // xuid -> reason
}

// LoadBans reads the ban set from path. If the file doesn't exist, an empty
// set is returned.
func LoadBans(path string) (*BanSet, error) {
	s := &BanSet{path: path, data: map[string]string{}}

	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return s, nil
	} else if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &s.data); err != nil {
		return nil, err
	}
	return s, nil
}

// Reason returns the ban reason for the given XUID and whether they're
// banned at all.
func (s *BanSet) Reason(xuid string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.data[xuid]
	return r, ok
}

// Ban bans the given XUID with the given reason and persists it.
func (s *BanSet) Ban(xuid, reason string) error {
	s.mu.Lock()
	s.data[xuid] = reason
	b, err := json.MarshalIndent(s.data, "", "  ")
	s.mu.Unlock()

	if err != nil {
		return err
	}
	return os.WriteFile(s.path, b, 0644)
}

// ---------------------------------------------------------------------
// Online player tracking (by name, for commands that target other players
// by name instead of a selector — e.g. /kick, /ban, /op, /tpto)
// ---------------------------------------------------------------------

var (
	onlineMu    sync.RWMutex
	onlinePlayers = map[string]*player.Player{} // lowercase name -> player
)

func trackJoin(p *player.Player) {
	onlineMu.Lock()
	onlinePlayers[lower(p.Name())] = p
	onlineMu.Unlock()
}

func trackQuit(p *player.Player) {
	onlineMu.Lock()
	delete(onlinePlayers, lower(p.Name()))
	onlineMu.Unlock()
}

func findOnline(name string) (*player.Player, bool) {
	onlineMu.RLock()
	defer onlineMu.RUnlock()
	p, ok := onlinePlayers[lower(name)]
	return p, ok
}

func lower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + ('a' - 'A')
		}
	}
	return string(b)
}


// PlayerHandler is attached to every player that joins the server. It
// embeds player.NopHandler so we only need to implement the methods we
// actually care about — every other event is a silent no-op until we add
// it here.
type PlayerHandler struct {
	player.NopHandler

	p     *player.Player
	ranks *RankSet
	log   *slog.Logger
}

// NewPlayerHandler creates a PlayerHandler for the given player.
func NewPlayerHandler(p *player.Player, ranks *RankSet, log *slog.Logger) *PlayerHandler {
	return &PlayerHandler{p: p, ranks: ranks, log: log}
}

// HandleQuit is called when the player disconnects, regardless of the
// reason. This is where you'd persist per-player state.
func (h *PlayerHandler) HandleQuit(p *player.Player) {
	trackQuit(p)
	h.log.Info("player quit", "name", p.Name(), "xuid", p.XUID())
}

// HandleChat tags every chat message with the player's rank prefix.
func (h *PlayerHandler) HandleChat(ctx *player.Context, message *string) {
	r := h.ranks.Of(h.p.XUID())
	*message = fmt.Sprintf("%s %s", r.ChatTag(), *message)
}

// HandleHurt is called every time the player takes damage, before it is
// applied. Example here: reduce fall damage by 50%.
func (h *PlayerHandler) HandleHurt(ctx *player.Context, damage *float64, immune bool, immunity *time.Duration, src world.DamageSource) {
	if _, ok := src.(entity.FallDamageSource); ok {
		*damage *= 0.5
	}
}

// HandleDeath is called when the player dies.
func (h *PlayerHandler) HandleDeath(p *player.Player, src world.DamageSource, keepInv *bool) {
	h.log.Info("player died", "name", p.Name())
}

// ---------------------------------------------------------------------
// Commands
// ---------------------------------------------------------------------

// Ping is a minimal example command: /ping just replies "Pong!".
type Ping struct{}

func (Ping) Run(src cmd.Source, output *cmd.Output, tx *world.Tx) {
	if p, ok := src.(*player.Player); ok {
		p.Message("§aPong!")
		return
	}
	output.Print("Pong!")
}

// GameModeEnum implements cmd.Enum so /gamemode can offer a dropdown of
// valid modes in the client's command UI instead of a free-typed string.
type GameModeEnum string

func (GameModeEnum) Type() string { return "GameMode" }
func (GameModeEnum) Options(cmd.Source) []string {
	return []string{"survival", "creative", "adventure", "spectator", "s", "c", "a", "sp"}
}

// resolveGameMode turns a full name or short letter into a world.GameMode.
// Returns false if the value isn't recognised.
func resolveGameMode(value string) (world.GameMode, bool) {
	switch value {
	case "survival", "s":
		return world.GameModeSurvival, true
	case "creative", "c":
		return world.GameModeCreative, true
	case "adventure", "a":
		return world.GameModeAdventure, true
	case "spectator", "sp":
		return world.GameModeSpectator, true
	}
	return nil, false
}

// GameMode is /gamemode <mode> — changes the executor's own game mode.
// Accepts either the full name (survival, creative, adventure, spectator)
// or the short form (s, c, a, sp). Op only.
// NOTE: this only affects the player running the command for now. Targeting
// other players (/gamemode creative Steve) needs cmd.Target support, which
// isn't wired up yet — ask if you want that added.
type GameMode struct {
	Mode GameModeEnum `cmd:"mode"`
}

func (GameMode) Allow(src cmd.Source) bool { return isOpSource(src) }
func (g GameMode) Run(src cmd.Source, output *cmd.Output, tx *world.Tx) {
	p, ok := src.(*player.Player)
	if !ok {
		output.Print("This command can only be run by a player.")
		return
	}
	mode, ok := resolveGameMode(string(g.Mode))
	if !ok {
		output.Printf("Unknown game mode: %s", g.Mode)
		return
	}
	p.SetGameMode(mode)
	p.Messagef("§aGame mode set to %s.", g.Mode)
}

// gmShortcut is shared logic for the /gms, /gmc, /gma, /gmsp quick commands
// below — each just sets a fixed game mode with no argument needed.
func gmShortcut(src cmd.Source, output *cmd.Output, mode world.GameMode, name string) {
	p, ok := src.(*player.Player)
	if !ok {
		output.Print("This command can only be run by a player.")
		return
	}
	p.SetGameMode(mode)
	p.Messagef("§aGame mode set to %s.", name)
}

// Gms is /gms — shortcut for /gamemode survival. Op only.
type Gms struct{}

func (Gms) Allow(src cmd.Source) bool { return isOpSource(src) }
func (Gms) Run(src cmd.Source, output *cmd.Output, tx *world.Tx) {
	gmShortcut(src, output, world.GameModeSurvival, "survival")
}

// Gmc is /gmc — shortcut for /gamemode creative. Op only.
type Gmc struct{}

func (Gmc) Allow(src cmd.Source) bool { return isOpSource(src) }
func (Gmc) Run(src cmd.Source, output *cmd.Output, tx *world.Tx) {
	gmShortcut(src, output, world.GameModeCreative, "creative")
}

// Gma is /gma — shortcut for /gamemode adventure. Op only.
type Gma struct{}

func (Gma) Allow(src cmd.Source) bool { return isOpSource(src) }
func (Gma) Run(src cmd.Source, output *cmd.Output, tx *world.Tx) {
	gmShortcut(src, output, world.GameModeAdventure, "adventure")
}

// Gmsp is /gmsp — shortcut for /gamemode spectator. Op only.
type Gmsp struct{}

func (Gmsp) Allow(src cmd.Source) bool { return isOpSource(src) }
func (Gmsp) Run(src cmd.Source, output *cmd.Output, tx *world.Tx) {
	gmShortcut(src, output, world.GameModeSpectator, "spectator")
}

// Tp is /tp <x> <y> <z> — teleports the executor to the given coordinates.
type Tp struct {
	Destination mgl64.Vec3 `cmd:"destination"`
}

func (Tp) Allow(src cmd.Source) bool { return isOpSource(src) }
func (t Tp) Run(src cmd.Source, output *cmd.Output, tx *world.Tx) {
	p, ok := src.(*player.Player)
	if !ok {
		output.Print("This command can only be run by a player.")
		return
	}
	p.Teleport(t.Destination)
	p.Messagef("§aTeleported to %.1f, %.1f, %.1f.", t.Destination[0], t.Destination[1], t.Destination[2])
}

// Feed is /feed — refills the executor's hunger and saturation to max.
type Feed struct{}

func (Feed) Allow(src cmd.Source) bool { return isOpSource(src) }
func (Feed) Run(src cmd.Source, output *cmd.Output, tx *world.Tx) {
	p, ok := src.(*player.Player)
	if !ok {
		output.Print("This command can only be run by a player.")
		return
	}
	p.SetFood(20)
	p.Message("§aHunger restored.")
}

// Coords is /coords — toggles the on-screen coordinate display.
type Coords struct{}

func (Coords) Run(src cmd.Source, output *cmd.Output, tx *world.Tx) {
	p, ok := src.(*player.Player)
	if !ok {
		output.Print("This command can only be run by a player.")
		return
	}
	coordsMu.Lock()
	shown := coordsState[p.XUID()]
	shown = !shown
	coordsState[p.XUID()] = shown
	coordsMu.Unlock()

	if shown {
		p.ShowCoordinates()
		p.Message("§aCoordinates shown.")
	} else {
		p.HideCoordinates()
		p.Message("§aCoordinates hidden.")
	}
}

// coordsState tracks whether each player (by XUID) currently has
// coordinates enabled, since /coords toggles it. Coordinates are shown by
// default on join (see main()).
var (
	coordsMu    sync.Mutex
	coordsState = map[string]bool{}
)

// globalOps and globalBans are set once in main() before the server starts
// accepting players, then read by command Allow/Run methods below.
var (
	globalOps  *OpSet
	globalBans *BanSet
)

// isOpSource reports whether the command source is allowed to run
// operator-only commands. Non-player sources (the server console) are
// always allowed; players must be on the op list.
func isOpSource(src cmd.Source) bool {
	p, ok := src.(*player.Player)
	if !ok {
		return true
	}
	return globalOps.IsOp(p.XUID())
}

// SetWorldSpawn is /setworldspawn — sets the world spawn to the executing
// player's current position. Op only.
type SetWorldSpawn struct{}

func (SetWorldSpawn) Allow(src cmd.Source) bool { return isOpSource(src) }
func (SetWorldSpawn) Run(src cmd.Source, output *cmd.Output, tx *world.Tx) {
	p, ok := src.(*player.Player)
	if !ok {
		output.Print("This command can only be run by a player.")
		return
	}
	// NOTE: cube.PosFromVec3 and World.SetSpawn are my best read of the
	// current API — if this line doesn't compile, send the exact error and
	// we'll fix the method/function name.
	pos := cube.PosFromVec3(p.Position())
	tx.World().SetSpawn(pos)
	output.Printf("Set the world spawn to %v.", pos)
}

// TimeEnum implements cmd.Enum for the /time set subcommand.
type TimeEnum string

func (TimeEnum) Type() string { return "TimeValue" }
func (TimeEnum) Options(cmd.Source) []string {
	return []string{"day", "noon", "night", "midnight"}
}

// TimeSet is /time set <value> — sets the world time of day. Op only.
type TimeSet struct {
	Set   cmd.SubCommand `cmd:"set"`
	Value TimeEnum       `cmd:"value"`
}

func (TimeSet) Allow(src cmd.Source) bool { return isOpSource(src) }
func (t TimeSet) Run(src cmd.Source, output *cmd.Output, tx *world.Tx) {
	var ticks int
	switch t.Value {
	case "day":
		ticks = 1000
	case "noon":
		ticks = 6000
	case "night":
		ticks = 13000
	case "midnight":
		ticks = 18000
	}
	tx.World().SetTime(ticks)
	output.Printf("Set the time to %s.", t.Value)
}

// TimeQuery is /time query — reports the current world time. Anyone can run
// this (not op-restricted).
type TimeQuery struct {
	Query cmd.SubCommand `cmd:"query"`
}

func (TimeQuery) Run(src cmd.Source, output *cmd.Output, tx *world.Tx) {
	output.Printf("The time is %d.", tx.World().Time())
}

// WeatherEnum implements cmd.Enum for /weather.
type WeatherEnum string

func (WeatherEnum) Type() string { return "Weather" }
func (WeatherEnum) Options(cmd.Source) []string {
	return []string{"clear", "rain", "thunder"}
}

// Weather is /weather <type> — changes the world weather. Op only.
type Weather struct {
	Type WeatherEnum `cmd:"type"`
}

func (Weather) Allow(src cmd.Source) bool { return isOpSource(src) }
func (w Weather) Run(src cmd.Source, output *cmd.Output, tx *world.Tx) {
	wo := tx.World()
	switch w.Type {
	case "clear":
		wo.StopRaining()
		wo.StopThundering()
	case "rain":
		wo.StartRaining(24 * time.Hour)
	case "thunder":
		wo.StartThundering(24 * time.Hour)
	}
	output.Printf("Set the weather to %s.", w.Type)
}

// Op is /op <player> — grants op to an online player by name. Op only.
type Op struct {
	Target string `cmd:"player"`
}

func (Op) Allow(src cmd.Source) bool { return isOpSource(src) }
func (o Op) Run(src cmd.Source, output *cmd.Output, tx *world.Tx) {
	target, ok := findOnline(o.Target)
	if !ok {
		output.Printf("Player '%s' is not online.", o.Target)
		return
	}
	if err := globalOps.SetOp(target.XUID(), true); err != nil {
		output.Printf("Failed to save op status: %v", err)
		return
	}
	target.Message("§aYou are now a server operator.")
	output.Printf("Made %s a server operator.", target.Name())
}

// Deop is /deop <player> — removes op from an online player by name.
// Op only.
type Deop struct {
	Target string `cmd:"player"`
}

func (Deop) Allow(src cmd.Source) bool { return isOpSource(src) }
func (d Deop) Run(src cmd.Source, output *cmd.Output, tx *world.Tx) {
	target, ok := findOnline(d.Target)
	if !ok {
		output.Printf("Player '%s' is not online.", d.Target)
		return
	}
	if err := globalOps.SetOp(target.XUID(), false); err != nil {
		output.Printf("Failed to save op status: %v", err)
		return
	}
	target.Message("§cYou are no longer a server operator.")
	output.Printf("Removed %s as a server operator.", target.Name())
}

// Kick is /kick <player> [reason] — disconnects an online player. Op only.
type Kick struct {
	Target string               `cmd:"player"`
	Reason cmd.Optional[string] `cmd:"reason"`
}

func (Kick) Allow(src cmd.Source) bool { return isOpSource(src) }
func (k Kick) Run(src cmd.Source, output *cmd.Output, tx *world.Tx) {
	target, ok := findOnline(k.Target)
	if !ok {
		output.Printf("Player '%s' is not online.", k.Target)
		return
	}
	reason := "Kicked by an operator."
	if r, ok := k.Reason.Load(); ok {
		reason = r
	}
	// NOTE: best guess at the disconnect method name — flag if this doesn't
	// compile and we'll swap it for the correct one.
	target.Disconnect(reason)
	output.Printf("Kicked %s: %s", target.Name(), reason)
}

// Ban is /ban <player> [reason] — bans an online player by XUID and
// disconnects them. The ban persists across sessions and is checked when
// anyone joins (see main()). Op only.
type Ban struct {
	Target string               `cmd:"player"`
	Reason cmd.Optional[string] `cmd:"reason"`
}

func (Ban) Allow(src cmd.Source) bool { return isOpSource(src) }
func (b Ban) Run(src cmd.Source, output *cmd.Output, tx *world.Tx) {
	target, ok := findOnline(b.Target)
	if !ok {
		output.Printf("Player '%s' is not online (only online players can be banned right now).", b.Target)
		return
	}
	reason := "Banned by an operator."
	if r, ok := b.Reason.Load(); ok {
		reason = r
	}
	if err := globalBans.Ban(target.XUID(), reason); err != nil {
		output.Printf("Failed to save ban: %v", err)
		return
	}
	target.Disconnect(reason)
	output.Printf("Banned %s: %s", target.Name(), reason)
}

// registerCommands registers every command. Call once before the server
// starts accepting players.
func registerCommands() {
	cmd.Register(cmd.New("ping", "Replies with Pong.", nil, Ping{}))
	cmd.Register(cmd.New("gamemode", "Changes your game mode.", []string{"gm"}, GameMode{}))
	cmd.Register(cmd.New("gms", "Switches you to survival mode.", nil, Gms{}))
	cmd.Register(cmd.New("gmc", "Switches you to creative mode.", nil, Gmc{}))
	cmd.Register(cmd.New("gma", "Switches you to adventure mode.", nil, Gma{}))
	cmd.Register(cmd.New("gmsp", "Switches you to spectator mode.", nil, Gmsp{}))
	cmd.Register(cmd.New("tp", "Teleports you to a set of coordinates.", []string{"teleport"}, Tp{}))
	cmd.Register(cmd.New("feed", "Restores your hunger.", nil, Feed{}))
	cmd.Register(cmd.New("coords", "Toggles the on-screen coordinate display.", []string{"xyz"}, Coords{}))
	cmd.Register(cmd.New("setworldspawn", "Sets the world spawn point.", nil, SetWorldSpawn{}))
	cmd.Register(cmd.New("time", "Changes or queries the world time.", nil, TimeSet{}, TimeQuery{}))
	cmd.Register(cmd.New("weather", "Changes the world weather.", nil, Weather{}))
	cmd.Register(cmd.New("op", "Grants a player operator status.", nil, Op{}))
	cmd.Register(cmd.New("deop", "Revokes a player's operator status.", nil, Deop{}))
	cmd.Register(cmd.New("kick", "Disconnects a player from the server.", nil, Kick{}))
	cmd.Register(cmd.New("ban", "Bans a player from the server.", nil, Ban{}))
}

// ---------------------------------------------------------------------
// Console command handling
// ---------------------------------------------------------------------
//
// NOTE: This section is my best-effort implementation and hasn't been
// compiled against the exact current API — the cmd.Source interface's
// required methods and world.World's transaction-execution method name are
// both educated guesses. If the build fails here, send the exact error and
// we'll correct the method/interface names — everything else in this file
// is unaffected either way.

// ConsoleSource implements cmd.Source for commands typed into the
// Pterodactyl console box (the process's stdin), as opposed to commands run
// by a connected player.
type ConsoleSource struct {
	w *world.World
}

func (ConsoleSource) Name() string           { return "CONSOLE" }
func (ConsoleSource) Position() mgl64.Vec3    { return mgl64.Vec3{} }
func (c ConsoleSource) World() *world.World   { return c.w }

// runConsole reads lines from stdin in a loop and executes each one as a
// command from the server console. Call this in its own goroutine from
// main() after srv.Listen().
func runConsole(w *world.World) {
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		name, args, _ := strings.Cut(line, " ")
		c, ok := cmd.ByAlias(name)
		if !ok {
			fmt.Println("Unknown command:", name)
			continue
		}
		w.Exec(func(tx *world.Tx) {
			c.Execute(args, ConsoleSource{w: w}, tx)
		})
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

	conf, err := userConf.Config(log)
	if err != nil {
		log.Error(err.Error())
		os.Exit(1)
	}

	conf.Allower = nil
	srv := conf.New()
	srv.CloseOnProgramEnd()

	chat.Global.Subscribe(chat.StdoutSubscriber{})

	registerCommands()

	ranks, err := LoadRanks("ranks.json")
	if err != nil {
		log.Error("failed to load ranks", "error", err.Error())
		os.Exit(1)
	}
	globalOps, err = LoadOps("ops.json")
	if err != nil {
		log.Error("failed to load ops", "error", err.Error())
		os.Exit(1)
	}
	globalBans, err = LoadBans("bans.json")
	if err != nil {
		log.Error("failed to load bans", "error", err.Error())
		os.Exit(1)
	}

	srv.Listen()
	go runConsole(srv.World())

	for p := range srv.Accept() {
		// Check the ban list before letting the player do anything else.
		if reason, banned := globalBans.Reason(p.XUID()); banned {
			p.Disconnect(reason)
			continue
		}

		// Auto-op the very first player to ever join, so there's always a
		// way to grant op in-game even with an empty ops.json.
		if globalOps.Empty() {
			_ = globalOps.SetOp(p.XUID(), true)
			p.Message("§aYou've been made a server operator (first join).")
		}

		trackJoin(p)
		p.Handle(NewPlayerHandler(p, ranks, log))

		// Basic server defaults on join: survival mode + coordinates shown.
		p.SetGameMode(world.GameModeSurvival)
		p.ShowCoordinates()
		coordsMu.Lock()
		coordsState[p.XUID()] = true
		coordsMu.Unlock()

		p.Message("§aWelcome to Velaris DragonFly!")
	}
}
