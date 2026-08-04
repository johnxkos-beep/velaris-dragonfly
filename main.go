package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/df-mc/dragonfly/server"
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
// Player event handler (PMMP event-listener equivalent)
// ---------------------------------------------------------------------

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
	return []string{"survival", "creative", "adventure", "spectator"}
}

// GameMode is /gamemode <mode> — changes the executor's own game mode.
// NOTE: this only affects the player running the command for now. Targeting
// other players (/gamemode creative Steve) needs cmd.Target support, which
// isn't wired up yet — ask if you want that added.
type GameMode struct {
	Mode GameModeEnum `cmd:"mode"`
}

func (g GameMode) Run(src cmd.Source, output *cmd.Output, tx *world.Tx) {
	p, ok := src.(*player.Player)
	if !ok {
		output.Print("This command can only be run by a player.")
		return
	}
	var mode world.GameMode
	switch g.Mode {
	case "survival":
		mode = world.GameModeSurvival
	case "creative":
		mode = world.GameModeCreative
	case "adventure":
		mode = world.GameModeAdventure
	case "spectator":
		mode = world.GameModeSpectator
	default:
		output.Printf("Unknown game mode: %s", g.Mode)
		return
	}
	p.SetGameMode(mode)
	p.Messagef("§aGame mode set to %s.", g.Mode)
}

// Tp is /tp <x> <y> <z> — teleports the executor to the given coordinates.
type Tp struct {
	Destination mgl64.Vec3 `cmd:"destination"`
}

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

// registerCommands registers every command. Call once before the server
// starts accepting players.
func registerCommands() {
	cmd.Register(cmd.New("ping", "Replies with Pong.", nil, Ping{}))
	cmd.Register(cmd.New("gamemode", "Changes your game mode.", []string{"gm"}, GameMode{}))
	cmd.Register(cmd.New("tp", "Teleports you to a set of coordinates.", []string{"teleport"}, Tp{}))
	cmd.Register(cmd.New("feed", "Restores your hunger.", nil, Feed{}))
	cmd.Register(cmd.New("coords", "Toggles the on-screen coordinate display.", []string{"xyz"}, Coords{}))
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

	srv.Listen()
	for p := range srv.Accept() {
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
