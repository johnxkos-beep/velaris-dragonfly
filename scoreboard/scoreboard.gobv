// Package scoreboard is a port of the Laith98Dev PocketMine-MP "Scoreboard"
// plugin (src/Laith98Dev/Scoreboard) to this Dragonfly stack. Same config
// shape (update-ticks / enabled / title / lines with {username} {online}
// {rank} {ping} wildcards), same "scoreboard.logo" title trick for the
// VelarisScoreboard resource pack's logo-swap panel, same refresh-loop +
// join/quit lifecycle.
//
// Not ported: the {clan} wildcard and its Clans plugin softdepend — this
// stack has no Clans equivalent. If a Clans-style plugin is ever built for
// Dragonfly, add a {clan} case to resolveLines the same way {rank} is
// handled below.
//
// Unlike the PMMP original (which had to hand-build SetDisplayObjective /
// SetScore packets and even reflect over the protocol's ScorePacketEntry
// type to survive breaking changes — see pocketmine-stack notes),
// Dragonfly ships a native server/player/scoreboard package, so this is
// built on Player.SendScoreboard / Player.RemoveScoreboard directly.
package scoreboard

import (
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/df-mc/dragonfly/server/player"
	dfscoreboard "github.com/df-mc/dragonfly/server/player/scoreboard"

	"velaris-dragonfly/ranks"
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
// Manager — send / update / remove, wildcard resolution, refresh loop
// ---------------------------------------------------------------------

// PlayerInfo is the small slice of *player.Player this package actually
// needs, kept as an interface so it doesn't have to import the full
// player.Player surface just for Name/Latency.
type PlayerInfo interface {
	Name() string
	Latency() time.Duration
}

// Manager renders and refreshes scoreboards for online players. It holds
// no player list of its own — Run is handed the current online list on
// every tick by the caller (main.go), same as every other periodic/lookup
// helper in this codebase (see state.FindAndActOnline's doc comment on
// why cached player pointers read fine off the world tx for simple,
// non-mutating sends like this one).
type Manager struct {
	cfg      *Config
	ranks    *ranks.RankSet
	rankDefs *ranks.RankDefSet
}

// NewManager creates a Manager backed by the given config and rank state.
func NewManager(cfg *Config, rks *ranks.RankSet, rankDefs *ranks.RankDefSet) *Manager {
	return &Manager{cfg: cfg, ranks: rks, rankDefs: rankDefs}
}

// Send builds the scoreboard from the current config and sends it to p.
// Call this on join and from the refresh loop. A no-op if the scoreboard
// is disabled in config.
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

// Remove clears p's scoreboard. Call this on quit.
func (m *Manager) Remove(p *player.Player) {
	p.RemoveScoreboard()
}

// resolveLines expands every wildcard in lines for p. There is no {clan}
// support (see package doc comment), so unlike the PMMP original there is
// no "drop the line if the wildcard has no value" case to handle.
func (m *Manager) resolveLines(p PlayerInfo, lines []string, onlineCount int) []string {
	out := make([]string, len(lines))
	for i, line := range lines {
		out[i] = m.resolveWildcards(p, line, onlineCount)
	}
	return out
}

func (m *Manager) resolveWildcards(p PlayerInfo, text string, onlineCount int) string {
	rank := ranks.DefaultRankName
	if def, ok := m.rankDefs.Get(m.ranks.Of(playerXUID(p))); ok {
		rank = def.Name
	}
	replacer := strings.NewReplacer(
		"{username}", p.Name(),
		"{online}", strconv.Itoa(onlineCount),
		"{rank}", rank,
		"{ping}", strconv.FormatInt(p.Latency().Milliseconds(), 10),
	)
	return replacer.Replace(text)
}

// playerXUID pulls the XUID off p if it exposes one (every real
// *player.Player does — this indirection only exists so PlayerInfo above
// can stay minimal for anything that doesn't need it, e.g. future tests).
func playerXUID(p PlayerInfo) string {
	type xuider interface{ XUID() string }
	if x, ok := p.(xuider); ok {
		return x.XUID()
	}
	return ""
}

// Run refreshes every online player's scoreboard on the config's
// update-ticks interval (in Bedrock ticks, 20 per second — same unit the
// original config.yml used) until stop is closed. players is called fresh
// on every tick so it always reflects who's currently online; pass
// something like func() []*player.Player { ... snapshot of
// state.Server.Players(nil) ... }.
func (m *Manager) Run(stop <-chan struct{}, players func() []*player.Player) {
	ticks := m.cfg.snapshot().UpdateTicks
	if ticks < 1 {
		ticks = 1
	}
	t := time.NewTicker(time.Duration(ticks) * 50 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			if !m.cfg.snapshot().Enabled {
				continue
			}
			online := players()
			for _, p := range online {
				m.Send(p, len(online))
			}
		}
	}
}
