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
	"github.com/df-mc/dragonfly/server/player/form"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// ---------------------------------------------------------------------
// Ranks
// ---------------------------------------------------------------------
//
// Two separate concerns live here:
//   - RankDefSet: the ranks that exist and how they're displayed — a name
//     plus two colors. TagColor colors the floating name tag shown above a
//     player in the world; ChatColor colors the prefix shown in the chat
//     window when they talk. Both are editable in-game via /rank (see the
//     "Rank Colors" section further down) and persisted to disk.
//   - RankSet: which rank each player (by XUID) currently has. Also
//     editable via /rank ("Set Rank" / "Remove Rank") and persisted.
//
// defaultRankName is what a player has until an op assigns them one.

const defaultRankName = "Default"

// RankDef is a single rank's display definition.
type RankDef struct {
	Name      string
	TagColor  string // §-code applied to the name tag shown above the player.
	ChatColor string // §-code applied to the prefix shown in chat.
}

// NameTagPrefix returns the text prepended to a player's floating name tag.
func (r RankDef) NameTagPrefix() string { return fmt.Sprintf("%s[%s]§r ", r.TagColor, r.Name) }

// ChatPrefix returns the text prepended to a player's chat messages.
func (r RankDef) ChatPrefix() string { return fmt.Sprintf("%s[%s]§r", r.ChatColor, r.Name) }

// RankDefSet holds every rank's definition and persists it to disk. Player
// -> rank assignment lives separately in RankSet, below.
type RankDefSet struct {
	mu    sync.RWMutex
	path  string
	defs  map[string]*RankDef
	order []string // display order for menus; also acts as the set of valid rank names
}

// LoadRankDefs reads rank definitions from the JSON file at path. If the
// file does not exist, four sensible defaults are seeded and saved.
func LoadRankDefs(path string) (*RankDefSet, error) {
	s := &RankDefSet{path: path, defs: map[string]*RankDef{}}

	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		defaults := []*RankDef{
			{Name: "Owner", TagColor: "§4", ChatColor: "§4"},
			{Name: "Admin", TagColor: "§c", ChatColor: "§c"},
			{Name: "YouTube", TagColor: "§c", ChatColor: "§c"},
			{Name: defaultRankName, TagColor: "§7", ChatColor: "§7"},
		}
		for _, d := range defaults {
			s.defs[d.Name] = d
			s.order = append(s.order, d.Name)
		}
		return s, s.save()
	} else if err != nil {
		return nil, err
	}

	var raw struct {
		Order []string
		Defs  map[string]*RankDef
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, err
	}
	s.order, s.defs = raw.Order, raw.Defs
	return s, nil
}

func (s *RankDefSet) save() error {
	raw := struct {
		Order []string
		Defs  map[string]*RankDef
	}{Order: s.order, Defs: s.defs}
	b, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, b, 0644)
}

// Names returns every rank name, in display order.
func (s *RankDefSet) Names() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, len(s.order))
	copy(out, s.order)
	return out
}

// Get returns the definition for the named rank, if it exists.
func (s *RankDefSet) Get(name string) (RankDef, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	d, ok := s.defs[name]
	if !ok {
		return RankDef{}, false
	}
	return *d, true
}

// SetTagColor changes a rank's name-tag color and persists the change.
func (s *RankDefSet) SetTagColor(name, code string) error {
	s.mu.Lock()
	d, ok := s.defs[name]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("unknown rank %q", name)
	}
	d.TagColor = code
	err := s.save()
	s.mu.Unlock()
	return err
}

// SetChatColor changes a rank's chat-prefix color and persists the change.
func (s *RankDefSet) SetChatColor(name, code string) error {
	s.mu.Lock()
	d, ok := s.defs[name]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("unknown rank %q", name)
	}
	d.ChatColor = code
	err := s.save()
	s.mu.Unlock()
	return err
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

// Of returns the rank name for the given XUID, falling back to
// defaultRankName if the player has no rank on file.
func (s *RankSet) Of(xuid string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	name, ok := s.data[xuid]
	if !ok {
		return defaultRankName
	}
	return name
}

// SetRank assigns a rank (by name) to the given XUID and persists the
// change to disk.
func (s *RankSet) SetRank(xuid, rankName string) error {
	s.mu.Lock()
	s.data[xuid] = rankName
	b, err := json.MarshalIndent(s.data, "", "  ")
	s.mu.Unlock()

	if err != nil {
		return err
	}
	return os.WriteFile(s.path, b, 0644)
}

// applyNameTag recomputes and sets a player's floating name tag from their
// current rank. Call this on join and any time a player's rank or a rank's
// TagColor changes.
func applyNameTag(p *player.Player, ranks *RankSet, defs *RankDefSet) {
	name := ranks.Of(p.XUID())
	def, ok := defs.Get(name)
	if !ok {
		def, _ = defs.Get(defaultRankName)
	}
	p.SetNameTag(def.NameTagPrefix() + p.Name())
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

// matchOnlineGreedy tries to match the longest possible prefix of args
// against an online player's name, so console commands work with
// multi-word names without needing quotes — e.g. "kick Velaris Founder
// being rude" correctly splits into name="Velaris Founder" and
// reason="being rude" by checking which online player it actually matches.
// Returns nil if no online player matches any prefix of args.
func matchOnlineGreedy(args []string) (*player.Player, []string) {
	onlineMu.RLock()
	defer onlineMu.RUnlock()
	for n := len(args); n >= 1; n-- {
		candidate := lower(strings.Join(args[:n], " "))
		if p, ok := onlinePlayers[candidate]; ok {
			return p, args[n:]
		}
	}
	return nil, nil
}

// findAndActOnline safely locates an online player by name (matching the
// longest possible prefix of args, so multi-word names work without
// quotes) and runs fn on it from within the transaction srv.Players opens
// for that player. This is required for anything that touches world/session
// state — e.g. Disconnect — since the *player.Player pointers cached in
// onlinePlayers are only safe to read simple fields from, not to mutate
// through, once execution is outside of their owning world transaction.
// Calling Disconnect directly on a cached pointer (as the old console kick
// case did) panics on dragonfly's internal tick goroutine, which no
// recover() in runConsole can catch, and takes the whole process down.
func findAndActOnline(srv *server.Server, args []string, fn func(p *player.Player, rest []string)) bool {
	for n := len(args); n >= 1; n-- {
		name := lower(strings.Join(args[:n], " "))
		for p := range srv.Players(nil) {
			if lower(p.Name()) == name {
				fn(p, args[n:])
				return true
			}
		}
	}
	return false
}

// PlayerHandler is attached to every player that joins the server. It
// embeds player.NopHandler so we only need to implement the methods we
// actually care about — every other event is a silent no-op until we add
// it here.
type PlayerHandler struct {
	player.NopHandler

	p        *player.Player
	ranks    *RankSet
	rankDefs *RankDefSet
	log      *slog.Logger
}

// NewPlayerHandler creates a PlayerHandler for the given player.
func NewPlayerHandler(p *player.Player, ranks *RankSet, rankDefs *RankDefSet, log *slog.Logger) *PlayerHandler {
	return &PlayerHandler{p: p, ranks: ranks, rankDefs: rankDefs, log: log}
}

// HandleQuit is called when the player disconnects, regardless of the
// reason. This is where you'd persist per-player state.
func (h *PlayerHandler) HandleQuit(p *player.Player) {
	trackQuit(p)
	h.log.Info("player quit", "name", p.Name(), "xuid", p.XUID())
}

// HandleChat tags every chat message with the player's rank prefix.
func (h *PlayerHandler) HandleChat(ctx *player.Context, message *string) {
	name := h.ranks.Of(h.p.XUID())
	def, ok := h.rankDefs.Get(name)
	if !ok {
		def, _ = h.rankDefs.Get(defaultRankName)
	}
	*message = fmt.Sprintf("%s %s", def.ChatPrefix(), *message)
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

// globalOps, globalBans, globalRanks, and globalRankDefs are set once in
// main() before the server starts accepting players, then read by command
// Allow/Run methods and form handlers below.
var (
	globalOps      *OpSet
	globalBans     *BanSet
	globalRanks    *RankSet
	globalRankDefs *RankDefSet
	globalServer   *server.Server
)

// findOnlineTx safely resolves an online player by name using the
// transaction tx belongs to, so the *player.Player returned is safe to
// mutate through — including calling Disconnect. This is required for
// anything (kick, ban, /rank's forms) that touches player/world state; the
// pointers cached by trackJoin/findOnline are only safe to read simple
// fields from once you're outside the transaction that owns them. See
// findAndActOnline's doc comment (used by the console) for the same
// concern from a goroutine with no tx at all.
func findOnlineTx(tx *world.Tx, name string) (*player.Player, bool) {
	target := lower(name)
	for p := range globalServer.Players(tx) {
		if lower(p.Name()) == target {
			return p, true
		}
	}
	return nil, false
}

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
	// findOnlineTx, not findOnline: Disconnect must be called on a player
	// reference that's valid within this tx, or it can panic on dragonfly's
	// internal tick goroutine instead of here. See findOnlineTx's comment.
	target, ok := findOnlineTx(tx, k.Target)
	if !ok {
		output.Printf("Player '%s' is not online.", k.Target)
		return
	}
	reason := "Kicked by an operator."
	if r, ok := k.Reason.Load(); ok {
		reason = r
	}
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
	// findOnlineTx, not findOnline — see Kick.Run's comment above.
	target, ok := findOnlineTx(tx, b.Target)
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

// ---------------------------------------------------------------------
// /rank — form-based rank management
// ---------------------------------------------------------------------
//
// /rank opens a root menu with three choices: Set Rank, Rank Colors, and
// Remove Rank. Each form's Submit runs inside the tx belonging to the
// admin who submitted it (see dragonfly's form docs), so every lookup here
// uses tx-bound srv.Players(tx) rather than the cached pointers in
// onlinePlayers — same safety concern as findOnlineTx above, just reached
// through globalServer instead of a passed-in srv.

// NOTE: form.Button's label is accessed via the exported Text string field
// (not a method) — confirmed after the initial build tried Text() and
// failed with "string is not a function".

// rankColorPalette is the fixed set of colors offered when recoloring a
// rank's tag or chat prefix.
var rankColorPalette = []struct{ Label, Code string }{
	{"Black", "§0"}, {"Dark Blue", "§1"}, {"Dark Green", "§2"}, {"Dark Aqua", "§3"},
	{"Dark Red", "§4"}, {"Dark Purple", "§5"}, {"Gold", "§6"}, {"Gray", "§7"},
	{"Dark Gray", "§8"}, {"Blue", "§9"}, {"Green", "§a"}, {"Aqua", "§b"},
	{"Red", "§c"}, {"Light Purple", "§d"}, {"Yellow", "§e"}, {"White", "§f"},
}

// findOnlinePlayerTx is a small wrapper around globalServer.Players(tx) for
// form Submit handlers, which already run inside a valid tx.
func findOnlinePlayerTx(tx *world.Tx, name string) (*player.Player, bool) {
	for p := range globalServer.Players(tx) {
		if p.Name() == name {
			return p, true
		}
	}
	return nil, false
}

// refreshTagsForRank updates the floating name tag of every online player
// currently holding rankName, e.g. after that rank's TagColor changes.
func refreshTagsForRank(tx *world.Tx, rankName string) {
	for p := range globalServer.Players(tx) {
		if globalRanks.Of(p.XUID()) == rankName {
			applyNameTag(p, globalRanks, globalRankDefs)
		}
	}
}

// --- Root menu: Set Rank / Rank Colors / Remove Rank ---

type rankRootForm struct{}

func (rankRootForm) Submit(submitter form.Submitter, pressed form.Button, tx *world.Tx) {
	p, ok := submitter.(*player.Player)
	if !ok {
		return
	}
	switch pressed.Text {
	case "Set Rank":
		sendRankTargetPicker(p, tx, "set")
	case "Rank Colors":
		sendRankColorTypeMenu(p, tx)
	case "Remove Rank":
		sendRankTargetPicker(p, tx, "remove")
	}
}

func sendRankRootMenu(p *player.Player) {
	menu := form.NewMenu(rankRootForm{}, "Rank Management").
		WithBody("Choose what you'd like to do.").
		WithButtons(
			form.NewButton("Set Rank", ""),
			form.NewButton("Rank Colors", ""),
			form.NewButton("Remove Rank", ""),
		)
	p.SendForm(menu)
}

// --- Player picker, shared by Set Rank and Remove Rank ---

type rankTargetForm struct{ mode string } // "set" or "remove"

func (f rankTargetForm) Submit(submitter form.Submitter, pressed form.Button, tx *world.Tx) {
	p, ok := submitter.(*player.Player)
	if !ok {
		return
	}
	target, ok := findOnlinePlayerTx(tx, pressed.Text)
	if !ok {
		p.Message("§cThat player is no longer online.")
		return
	}
	if f.mode == "remove" {
		if err := globalRanks.SetRank(target.XUID(), defaultRankName); err != nil {
			p.Message("§cFailed to save rank: " + err.Error())
			return
		}
		applyNameTag(target, globalRanks, globalRankDefs)
		target.Message("§7Your rank has been reset to Default.")
		p.Message(fmt.Sprintf("§aReset %s's rank to Default.", target.Name()))
		return
	}
	sendRankPicker(p, tx, target.Name())
}

func sendRankTargetPicker(p *player.Player, tx *world.Tx, mode string) {
	var buttons []form.Button
	for other := range globalServer.Players(tx) {
		buttons = append(buttons, form.NewButton(other.Name(), ""))
	}
	if len(buttons) == 0 {
		p.Message("§cNo players are online.")
		return
	}
	title := "Set Rank — pick a player"
	if mode == "remove" {
		title = "Remove Rank — pick a player"
	}
	menu := form.NewMenu(rankTargetForm{mode: mode}, title).
		WithBody("Select a player.").
		WithButtons(buttons...)
	p.SendForm(menu)
}

// --- Rank picker (after choosing a target for "Set Rank") ---

type rankAssignForm struct{ targetName string }

func (f rankAssignForm) Submit(submitter form.Submitter, pressed form.Button, tx *world.Tx) {
	p, ok := submitter.(*player.Player)
	if !ok {
		return
	}
	rankName := pressed.Text
	if _, ok := globalRankDefs.Get(rankName); !ok {
		p.Message("§cUnknown rank.")
		return
	}
	target, ok := findOnlinePlayerTx(tx, f.targetName)
	if !ok {
		p.Message("§cThat player is no longer online.")
		return
	}
	if err := globalRanks.SetRank(target.XUID(), rankName); err != nil {
		p.Message("§cFailed to save rank: " + err.Error())
		return
	}
	applyNameTag(target, globalRanks, globalRankDefs)
	target.Message("§aYour rank has been set to " + rankName + ".")
	p.Message(fmt.Sprintf("§aSet %s's rank to %s.", target.Name(), rankName))
}

func sendRankPicker(p *player.Player, tx *world.Tx, targetName string) {
	var buttons []form.Button
	for _, name := range globalRankDefs.Names() {
		buttons = append(buttons, form.NewButton(name, ""))
	}
	menu := form.NewMenu(rankAssignForm{targetName: targetName}, fmt.Sprintf("Set Rank — %s", targetName)).
		WithBody("Choose a rank to assign.").
		WithButtons(buttons...)
	p.SendForm(menu)
}

// --- Color type menu: Tag Color vs Chat Color ---

type rankColorTypeForm struct{}

func (rankColorTypeForm) Submit(submitter form.Submitter, pressed form.Button, tx *world.Tx) {
	p, ok := submitter.(*player.Player)
	if !ok {
		return
	}
	switch pressed.Text {
	case "Tag Color (above head)":
		sendRankColorRankPicker(p, tx, "tag")
	case "Chat Color (in chat)":
		sendRankColorRankPicker(p, tx, "chat")
	}
}

func sendRankColorTypeMenu(p *player.Player, tx *world.Tx) {
	menu := form.NewMenu(rankColorTypeForm{}, "Rank Colors").
		WithBody("Tag Color is the floating name tag shown above a player in the world. Chat Color is the prefix shown when they type in chat.").
		WithButtons(
			form.NewButton("Tag Color (above head)", ""),
			form.NewButton("Chat Color (in chat)", ""),
		)
	p.SendForm(menu)
}

// --- Rank picker for recoloring ---

type rankColorRankForm struct{ kind string } // "tag" or "chat"

func (f rankColorRankForm) Submit(submitter form.Submitter, pressed form.Button, tx *world.Tx) {
	p, ok := submitter.(*player.Player)
	if !ok {
		return
	}
	if _, ok := globalRankDefs.Get(pressed.Text); !ok {
		p.Message("§cUnknown rank.")
		return
	}
	sendRankColorSwatchPicker(p, tx, f.kind, pressed.Text)
}

func sendRankColorRankPicker(p *player.Player, tx *world.Tx, kind string) {
	var buttons []form.Button
	for _, name := range globalRankDefs.Names() {
		buttons = append(buttons, form.NewButton(name, ""))
	}
	menu := form.NewMenu(rankColorRankForm{kind: kind}, "Pick a rank to recolor").
		WithBody("Choose which rank's color to change.").
		WithButtons(buttons...)
	p.SendForm(menu)
}

// --- Color swatch picker (final step) ---

type rankColorSwatchForm struct {
	kind     string // "tag" or "chat"
	rankName string
}

func (f rankColorSwatchForm) Submit(submitter form.Submitter, pressed form.Button, tx *world.Tx) {
	p, ok := submitter.(*player.Player)
	if !ok {
		return
	}
	var code string
	for _, c := range rankColorPalette {
		if c.Label == pressed.Text {
			code = c.Code
			break
		}
	}
	if code == "" {
		p.Message("§cUnknown color.")
		return
	}

	var err error
	if f.kind == "tag" {
		err = globalRankDefs.SetTagColor(f.rankName, code)
	} else {
		err = globalRankDefs.SetChatColor(f.rankName, code)
	}
	if err != nil {
		p.Message("§cFailed to save color: " + err.Error())
		return
	}
	if f.kind == "tag" {
		refreshTagsForRank(tx, f.rankName)
	}
	p.Message(fmt.Sprintf("§aUpdated %s's %s color.", f.rankName, f.kind))
}

func sendRankColorSwatchPicker(p *player.Player, tx *world.Tx, kind, rankName string) {
	var buttons []form.Button
	for _, c := range rankColorPalette {
		buttons = append(buttons, form.NewButton(c.Label, ""))
	}
	menu := form.NewMenu(rankColorSwatchForm{kind: kind, rankName: rankName}, fmt.Sprintf("Pick a color for %s", rankName)).
		WithBody("Choose a color.").
		WithButtons(buttons...)
	p.SendForm(menu)
}

// RankMenu is /rank — opens the rank management menu. In-game only, op only.
type RankMenu struct{}

func (RankMenu) Allow(src cmd.Source) bool { return isOpSource(src) }
func (RankMenu) Run(src cmd.Source, output *cmd.Output, tx *world.Tx) {
	p, ok := src.(*player.Player)
	if !ok {
		output.Print("The /rank menu can only be opened in-game.")
		return
	}
	sendRankRootMenu(p)
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
	cmd.Register(cmd.New("rank", "Opens the rank management menu.", nil, RankMenu{}))
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
// findAndActOnline so it runs inside a proper transaction — see its doc
// comment for why that matters. Ban is in-game only for now (see Ban.Run).

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
			// mutates player/world state must go through findAndActOnline
			// (see its doc comment) or a panic there will happen on
			// dragonfly's internal tick goroutine instead, where this
			// recover can't reach it.
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
				target, _ := matchOnlineGreedy(args)
				if target == nil {
					fmt.Println("No online player matches that name.")
					return
				}
				if err := globalOps.SetOp(target.XUID(), true); err != nil {
					fmt.Println("Failed to save op status:", err)
					return
				}
				target.Message("§aYou are now a server operator.")
				fmt.Printf("Made %s a server operator.\n", target.Name())

			case "deop":
				target, _ := matchOnlineGreedy(args)
				if target == nil {
					fmt.Println("No online player matches that name.")
					return
				}
				if err := globalOps.SetOp(target.XUID(), false); err != nil {
					fmt.Println("Failed to save op status:", err)
					return
				}
				target.Message("§cYou are no longer a server operator.")
				fmt.Printf("Removed %s as a server operator.\n", target.Name())

			case "kick":
				reason := "Kicked by an operator."
				found := findAndActOnline(srv, args, func(target *player.Player, rest []string) {
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

	conf, err := userConf.Config(log)
	if err != nil {
		log.Error(err.Error())
		os.Exit(1)
	}

	conf.Allower = nil
	srv := conf.New()
	srv.CloseOnProgramEnd()
	globalServer = srv

	chat.Global.Subscribe(chat.StdoutSubscriber{})

	registerCommands()

	globalRanks, err = LoadRanks("ranks.json")
	if err != nil {
		log.Error("failed to load ranks", "error", err.Error())
		os.Exit(1)
	}
	globalRankDefs, err = LoadRankDefs("rank_defs.json")
	if err != nil {
		log.Error("failed to load rank definitions", "error", err.Error())
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
	go runConsole(srv)

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
		p.Handle(NewPlayerHandler(p, globalRanks, globalRankDefs, log))
		applyNameTag(p, globalRanks, globalRankDefs)

		// Basic server defaults on join: survival mode + coordinates shown.
		p.SetGameMode(world.GameModeSurvival)
		p.ShowCoordinates()
		coordsMu.Lock()
		coordsState[p.XUID()] = true
		coordsMu.Unlock()

		p.Message("§aWelcome to Velaris DragonFly!")
	}
}
