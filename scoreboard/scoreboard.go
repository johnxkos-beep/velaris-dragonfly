// Package scoreboard is a port of the Laith98Dev PocketMine-MP "Scoreboard"
// plugin (src/Laith98Dev/Scoreboard) to this Dragonfly stack. Same config
// shape (update-ticks / enabled / title / lines with {username} {online}
// {rank} {ping} wildcards), same "scoreboard.logo" title trick for the
// VelarisScoreboard resource pack's logo-swap panel, same refresh-loop +
// join/quit lifecycle.
//
// Not ported: the {clan} wildcard and its Clans plugin softdepend — this
// stack has no Clans equivalent. In its place, {team} resolves against
// this project's own `teams` package (the HopliteLegendary port — see
// teams/manager.go) the same way {clan} would have against Clans: the
// player's current team name in that team's own color, or "None" if
// they're not in one.
//
// Unlike the PMMP original (which had to hand-build SetDisplayObjective /
// SetScore packets and even reflect over the protocol's ScorePacketEntry
// type to survive breaking changes — see pocketmine-stack notes),
// Dragonfly ships a native server/player/scoreboard package, so this is
// built on Player.SendScoreboard / Player.RemoveScoreboard directly.
//
// The refresh loop is a single invisible ticker entity (see ticker below),
// not a background goroutine — an earlier version of this file used
// world.World.Exec for this and it doesn't compile against the version of
// Dragonfly this repo is pinned to (only an unexported (*World).exec
// exists). This mirrors the exact pattern this codebase already uses
// twice for the same class of problem: restrict's enforcer entity
// (restrict/restrict.go) and legendary's eagle-draw ticker — both spawn a
// single Tick(tx, current)-driven entity instead of touching players from
// an independent goroutine, after this repo hit real, silent
// ClientDisconnection failures doing exactly that.
package scoreboard

import (
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/player"
	dfscoreboard "github.com/df-mc/dragonfly/server/player/scoreboard"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"

	"velaris-dragonfly/ranks"
	"velaris-dragonfly/teams"
)

// settings mirrors the original plugin's resources/config.yml field for
// field (see that file's comments for what each does).
type settings struct {
	UpdateTicks int      `json:"update-ticks"`
	Enabled     bool     `json:"enabled"`
	Title       string   `json:"title"`
	Lines       []string `json:"lines"`
}

// defaultSettings mirrors the stock config.yml shipped with the original
// Scoreboard plugin exactly, minus the {clan} line (no Clans plugin on
// this stack — see the package doc comment).
func defaultSettings() settings {
	return settings{
		UpdateTicks: 20,
		Enabled:     true,
		// "scoreboard.logo" is not shown as literal text — it's the exact
		// objective name the VelarisScoreboard(.mcpack) resource pack's
		// ui/scoreboards.json checks for ("#objective_sidebar_name =
		// 'scoreboard.logo'") to swap the title text for the logo image.
		// Keep this pack loaded (Resources.Folder, see main.go) for this
		// to render as a logo instead of literal text.
		Title: "scoreboard.logo",
		Lines: []string{
			"§b{username}",
			"§f",
			"§bRank: §f{rank}",
			"§bTeam: §f{team}",
			"§bPing: §f{ping}ms",
			"§f",
			"§bOnline: §f{online}",
		},
	}
}

// Config is the active, hot-editable scoreboard configuration. Safe for
// concurrent use from the update loop and any future in-game editor.
type Config struct {
	mu   sync.RWMutex
	path string
	s    settings
}

// Load reads the config from the JSON file at path, creating it with
// defaults (identical to the original plugin's config.yml) if it doesn't
// exist yet. Call this once from main() before srv.Accept(), same pattern
// as knockback.Load / pvp.Load / restrict.Load.
func Load(path string) (*Config, error) {
	c := &Config{path: path, s: defaultSettings()}

	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return c, c.save(c.s)
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &c.s); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *Config) snapshot() settings {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.s
}

func (c *Config) save(s settings) error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(c.path, b, 0644)
}

// ---------------------------------------------------------------------
// Manager — send / update / remove, wildcard resolution
// ---------------------------------------------------------------------

// Manager renders and refreshes scoreboards for online players.
type Manager struct {
	cfg      *Config
	ranks    *ranks.RankSet
	rankDefs *ranks.RankDefSet

	mu            sync.Mutex
	tickerSpawned bool
}

// active is the single live Manager, read by ticker.Tick — same
// package-global pattern restrict.go uses for its enforcer (Cfg read
// directly from enforcer.Tick rather than threaded through the entity).
var active *Manager

// NewManager creates a Manager backed by the given config and rank state.
func NewManager(cfg *Config, rks *ranks.RankSet, rankDefs *ranks.RankDefSet) *Manager {
	m := &Manager{cfg: cfg, ranks: rks, rankDefs: rankDefs}
	active = m
	return m
}

// Send builds the scoreboard from the current config and sends it to p.
// Call this on join and from the ticker's refresh. A no-op if the
// scoreboard is disabled in config.
//
// p must be a *player.Player that is currently valid — i.e. you're calling
// this either (a) synchronously inside the same for-range block that
// produced p (srv.Accept(), srv.Players(nil), tx.Players()...), or (b)
// inside a world.Tx-owning Tick callback that owns p's transaction.
// Stashing p in a slice/closure and calling this later, from a different
// goroutine, is undefined behaviour per Dragonfly's own docs on
// Players()/Accept() — that was a real bug in an earlier version of this
// file (see ticker.Tick below for the fix, mirroring restrict.go's
// enforcer and the ClientDisconnection history documented there).
func (m *Manager) Send(p *player.Player, onlineCount int) {
	s := m.cfg.snapshot()
	if !s.Enabled {
		return
	}
	title := m.resolveWildcards(p, s.Title, onlineCount)
	board := dfscoreboard.New(title)
	board.RemovePadding() // the original plugin's lines have no leading indent
	for i, line := range m.resolveLines(p, s.Lines, onlineCount) {
		board.Set(i, line)
	}
	p.SendScoreboard(board)
}

// Remove clears p's scoreboard. Call this on quit. Same p-validity rule as
// Send applies.
func (m *Manager) Remove(p *player.Player) {
	p.RemoveScoreboard()
}

// resolveLines expands every wildcard in lines for p. There is no {clan}
// support (see package doc comment), so unlike the PMMP original there is
// no "drop the line if the wildcard has no value" case to handle.
func (m *Manager) resolveLines(p *player.Player, lines []string, onlineCount int) []string {
	out := make([]string, len(lines))
	for i, line := range lines {
		out[i] = m.resolveWildcards(p, line, onlineCount)
	}
	return out
}

func (m *Manager) resolveWildcards(p *player.Player, text string, onlineCount int) string {
	rank := ranks.DefaultRankName
	if def, ok := m.rankDefs.Get(m.ranks.Of(p.XUID())); ok {
		rank = def.Name
	}
	team := "None"
	if teams.Mgr != nil {
		if t, ok := teams.Mgr.GetTeamOfPlayer(p.Name()); ok {
			team = t.Color + t.Name
		}
	}
	replacer := strings.NewReplacer(
		"{username}", p.Name(),
		"{online}", strconv.Itoa(onlineCount),
		"{rank}", rank,
		"{team}", team,
		"{ping}", strconv.FormatInt(p.Latency().Milliseconds(), 10),
	)
	return replacer.Replace(text)
}

// EnsureTicker spawns the single scoreboard-refresh ticker entity the
// first time it's needed. Safe to call repeatedly; only spawns once. Call
// it from the very first real *world.Tx you have — e.g. right after the
// first player joins, using p.Tx() and p.Position() (see main.go). Same
// "spawn near actual player activity, not a fixed coordinate" reasoning
// as restrict.go's ensureEnforcer: the Tick method only fires while its
// own chunk is loaded, so spawning it next to a real player guarantees
// that.
func (m *Manager) EnsureTicker(tx *world.Tx, near mgl64.Vec3) {
	m.mu.Lock()
	if m.tickerSpawned {
		m.mu.Unlock()
		return
	}
	m.tickerSpawned = true
	m.mu.Unlock()

	handle := world.EntitySpawnOpts{Position: near}.New(TickerType, tickerConfig{})
	tx.AddEntity(handle)
}

// ---------------------------------------------------------------------
// ticker: a single, invisible, always-on entity whose only job is
// refreshing every online player's scoreboard on config's update-ticks
// interval. Exactly one is ever spawned — see Manager.EnsureTicker.
// ---------------------------------------------------------------------

// TickerType is the entity type for the invisible scoreboard ticker.
var TickerType tickerType

// EntityTypes returns the entity types this package adds, for merging
// into the server's entity registry — see the wiring note in main.go
// (mirrors restrict.EntityTypes() / demonking.EntityRegistry()).
func EntityTypes() []world.EntityType { return []world.EntityType{TickerType} }

var tickerBBox = cube.Box(0, 0, 0, 0, 0, 0)

type tickerType struct{}

func (tickerType) EncodeEntity() string        { return "velaris:scoreboard_ticker" }
func (tickerType) BBox(world.Entity) cube.BBox { return tickerBBox }
func (tickerType) Open(tx *world.Tx, handle *world.EntityHandle, data *world.EntityData) world.Entity {
	return &ticker{tx: tx, handle: handle, data: data}
}
func (tickerType) DecodeNBT(m map[string]any, data *world.EntityData) {}
func (tickerType) EncodeNBT(data *world.EntityData) map[string]any    { return map[string]any{} }

// tickerConfig is an empty EntitySpawnOpts config for TickerType, which
// needs no spawn-time configuration — mirrors restrict's enforcerConfig.
type tickerConfig struct{}

func (tickerConfig) Apply(data *world.EntityData) {}

type ticker struct {
	tx     *world.Tx
	handle *world.EntityHandle
	data   *world.EntityData
}

func (t *ticker) H() *world.EntityHandle  { return t.handle }
func (t *ticker) Position() mgl64.Vec3    { return t.data.Pos }
func (t *ticker) Rotation() cube.Rotation { return t.data.Rot }
func (t *ticker) Close() error            { return nil }

// Tick refreshes every online player's scoreboard every UpdateTicks
// server ticks (20 ticks/sec, same unit the original config.yml used).
func (t *ticker) Tick(tx *world.Tx, current int64) {
	if active == nil {
		return
	}
	s := active.cfg.snapshot()
	if !s.Enabled {
		return
	}
	ticks := int64(s.UpdateTicks)
	if ticks < 1 {
		ticks = 1
	}
	if current%ticks != 0 {
		return
	}

	count := 0
	for e := range tx.Players() {
		if _, ok := e.(*player.Player); ok {
			count++
		}
	}
	for e := range tx.Players() {
		if p, ok := e.(*player.Player); ok {
			active.Send(p, count)
		}
	}
}
