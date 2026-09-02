// Package teams is a port of the HopliteLegendary PocketMine-MP plugin's
// team system (hoplite\core\teams: TeamManager, TeamCommand, TeamListener)
// to Dragonfly: create/join/leave/kick/disband teams, a per-team owner,
// team-only chat, a per-team friendly-fire toggle, and per-member
// kill/death tracking — all driven from /team's form menu, plus
// /team chat [message] for team chat. See command.go, forms.go, and
// hooks.go for the rest of the package; this file is just the data model
// and persistence, ported from TeamManager.php.
//
// DEVIATION FROM THE REST OF THIS PROJECT: every other persisted store
// here (ranks, ops, bans) is keyed by XUID, since names can change across
// sessions. Teams are kept keyed by player name (lowercase) instead,
// exactly like the PHP original, because the original explicitly supports
// inviting a player who isn't online yet by typing their name — something
// that only works if invites are name-keyed, since there's no XUID to look
// up for someone who has never connected. If that offline-invite behavior
// turns out not to matter to you in practice, switching this to XUID keys
// throughout would be a fairly mechanical follow-up change.
package teams

import (
	"encoding/json"
	"os"
	"strings"
	"sync"
)

// MaxMembers is the maximum number of players a single team can hold.
// Matches TeamManager::MAX_MEMBERS in the PHP original.
const MaxMembers = 5

// ColorOption is one entry in the fixed nametag-color palette offered when
// creating a team, ported field-for-field from TeamCommand::COLORS /
// COLOR_CODES in the PHP original.
type ColorOption struct {
	Label string
	Code  string // §-code
}

// Colors is the fixed palette of team nametag colors, in the same order
// the PHP original presented them in its "Create a Team" dropdown.
var Colors = []ColorOption{
	{"Red", "§c"},
	{"Blue", "§9"},
	{"Green", "§a"},
	{"Yellow", "§e"},
	{"Aqua", "§b"},
	{"Light Purple", "§d"},
	{"Gold", "§6"},
	{"White", "§f"},
}

// ColorNames returns just the labels from Colors, in order — the option
// list a form.Dropdown needs.
func ColorNames() []string {
	names := make([]string, len(Colors))
	for i, c := range Colors {
		names[i] = c.Label
	}
	return names
}

// colorCode looks up the §-code for a color label from the palette above.
// Falls back to white if label isn't recognised (shouldn't happen — the
// dropdown only ever offers labels from Colors).
func colorCode(label string) string {
	for _, c := range Colors {
		if c.Label == label {
			return c.Code
		}
	}
	return "§f"
}

// Team is one team's full state. Exported fields are persisted to disk as
// part of Manager's teams.json, and read directly by forms.go/hooks.go —
// callers must not mutate a Team obtained from Manager without going
// through a Manager method, since Manager's mutex protects the underlying
// maps/slices, not the returned copies. GetTeam/GetTeamOfPlayer return
// copies for exactly this reason (see the PHP original's array-based
// teams, which were copy-by-value on every read for the same effect).
type Team struct {
	Name         string         `json:"name"`
	Owner        string         `json:"owner"` // player name, not lowercased
	Color        string         `json:"color"` // §-code
	FriendlyFire bool           `json:"friendlyFire"`
	Members      []string       `json:"members"` // player names, not lowercased
	Kills        map[string]int `json:"kills"`   // player name -> kill count
	Deaths       map[string]int `json:"deaths"`  // player name -> death count
}

// clone returns a deep-enough copy of t safe to hand to a caller outside
// Manager's lock (Members/Kills/Deaths are copied; the strings within are
// immutable in Go so a shallow copy of those is fine).
func (t Team) clone() Team {
	members := make([]string, len(t.Members))
	copy(members, t.Members)
	kills := make(map[string]int, len(t.Kills))
	for k, v := range t.Kills {
		kills[k] = v
	}
	deaths := make(map[string]int, len(t.Deaths))
	for k, v := range t.Deaths {
		deaths[k] = v
	}
	return Team{
		Name: t.Name, Owner: t.Owner, Color: t.Color, FriendlyFire: t.FriendlyFire,
		Members: members, Kills: kills, Deaths: deaths,
	}
}

// persisted is the on-disk shape of teams.json — mirrors the three arrays
// TeamManager::load()/save() round-trip in the PHP original.
type persisted struct {
	Teams      map[string]*Team    `json:"teams"`
	PlayerTeam map[string]string   `json:"playerTeam"`
	Invites    map[string][]string `json:"invites"`
}

// Manager holds every team, the player->team index, and pending invites,
// and persists all three to a single JSON file — a direct port of
// TeamManager.php's in-memory arrays plus its load()/save().
type Manager struct {
	mu         sync.Mutex
	path       string
	teams      map[string]*Team
	playerTeam map[string]string
	invites    map[string][]string
}

// Mgr is the single active Manager, set once in main() via teams.Load
// before the server starts accepting players — same pattern as
// state.Ranks / state.Ops / knockback.Cfg. Every other file in this
// package, and every caller outside it, reads/writes through this.
var Mgr *Manager

// Load reads team data from the JSON file at path, creating an empty file
// if it doesn't exist yet. Call this once from main() before srv.Accept(),
// then assign the result to teams.Mgr, e.g.:
//
//	teams.Mgr, err = teams.Load("teams.json")
func Load(path string) (*Manager, error) {
	m := &Manager{
		path:       path,
		teams:      map[string]*Team{},
		playerTeam: map[string]string{},
		invites:    map[string][]string{},
	}

	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return m, m.save()
	}
	if err != nil {
		return nil, err
	}
	var p persisted
	if err := json.Unmarshal(b, &p); err != nil {
		return nil, err
	}
	if p.Teams != nil {
		m.teams = p.Teams
	}
	if p.PlayerTeam != nil {
		m.playerTeam = p.PlayerTeam
	}
	if p.Invites != nil {
		m.invites = p.Invites
	}
	// Older/partially-written files might be missing the Kills/Deaths
	// maps on a team (they were added mid-way in the PHP original too);
	// guard against a nil map panicking on first write.
	for _, t := range m.teams {
		if t.Kills == nil {
			t.Kills = map[string]int{}
		}
		if t.Deaths == nil {
			t.Deaths = map[string]int{}
		}
	}
	return m, nil
}

func (m *Manager) save() error {
	b, err := json.MarshalIndent(persisted{
		Teams:      m.teams,
		PlayerTeam: m.playerTeam,
		Invites:    m.invites,
	}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(m.path, b, 0644)
}

func lower(s string) string { return strings.ToLower(s) }

// GetTeam returns a copy of the named team, or false if it doesn't exist.
func (m *Manager) GetTeam(name string) (Team, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.teams[name]
	if !ok {
		return Team{}, false
	}
	return t.clone(), true
}

// GetTeamOfPlayer returns a copy of playerName's current team, or false if
// they're not in one.
func (m *Manager) GetTeamOfPlayer(playerName string) (Team, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	teamName, ok := m.playerTeam[lower(playerName)]
	if !ok {
		return Team{}, false
	}
	t, ok := m.teams[teamName]
	if !ok {
		return Team{}, false
	}
	return t.clone(), true
}

// IsOwner reports whether playerName owns teamName.
func (m *Manager) IsOwner(playerName, teamName string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.teams[teamName]
	return ok && lower(t.Owner) == lower(playerName)
}

// CreateTeam creates a new team owned by owner. Returns "" on success, or
// an error message identical in wording to the PHP original.
func (m *Manager) CreateTeam(owner, teamName, color string) string {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.playerTeam[lower(owner)]; ok {
		return "You're already in a team."
	}
	if _, ok := m.teams[teamName]; ok {
		return "That team name is already taken."
	}
	if strings.TrimSpace(teamName) == "" || len(teamName) > 16 {
		return "Team names must be 1-16 characters."
	}

	m.teams[teamName] = &Team{
		Name: teamName, Owner: owner, Color: color, FriendlyFire: false,
		Members: []string{owner}, Kills: map[string]int{}, Deaths: map[string]int{},
	}
	m.playerTeam[lower(owner)] = teamName
	if err := m.save(); err != nil {
		return "Failed to save: " + err.Error()
	}
	return ""
}

// Disband removes owner's team entirely. Returns "" on success.
func (m *Manager) Disband(owner string) string {
	m.mu.Lock()
	defer m.mu.Unlock()

	teamName, ok := m.playerTeam[lower(owner)]
	if !ok {
		return "You don't own a team."
	}
	t := m.teams[teamName]
	if t == nil || lower(t.Owner) != lower(owner) {
		return "You don't own a team."
	}
	for _, member := range t.Members {
		delete(m.playerTeam, lower(member))
	}
	delete(m.teams, teamName)
	if err := m.save(); err != nil {
		return "Failed to save: " + err.Error()
	}
	return ""
}

// Invite records an invite from owner's team to target. Returns "" on
// success.
func (m *Manager) Invite(owner, target string) string {
	m.mu.Lock()
	defer m.mu.Unlock()

	teamName, ok := m.playerTeam[lower(owner)]
	if !ok {
		return "You don't own a team."
	}
	t := m.teams[teamName]
	if t == nil || lower(t.Owner) != lower(owner) {
		return "You don't own a team."
	}
	if len(t.Members) >= MaxMembers {
		return "Your team is already full (5 members)."
	}
	if _, ok := m.playerTeam[lower(target)]; ok {
		return target + " is already in a team."
	}
	key := lower(target)
	found := false
	for _, name := range m.invites[key] {
		if name == t.Name {
			found = true
			break
		}
	}
	if !found {
		m.invites[key] = append(m.invites[key], t.Name)
	}
	if err := m.save(); err != nil {
		return "Failed to save: " + err.Error()
	}
	return ""
}

// GetInvites returns the team names that have invited playerName.
func (m *Manager) GetInvites(playerName string) []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	src := m.invites[lower(playerName)]
	out := make([]string, len(src))
	copy(out, src)
	return out
}

// AcceptInvite joins playerName to teamName, provided they were actually
// invited. Returns "" on success.
func (m *Manager) AcceptInvite(playerName, teamName string) string {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := lower(playerName)
	invited := false
	for _, name := range m.invites[key] {
		if name == teamName {
			invited = true
			break
		}
	}
	if !invited {
		return "You don't have an invite from that team."
	}
	if _, ok := m.playerTeam[key]; ok {
		return "You're already in a team."
	}
	t, ok := m.teams[teamName]
	if !ok {
		delete(m.invites, key)
		return "That team no longer exists."
	}
	if len(t.Members) >= MaxMembers {
		return "That team is now full."
	}

	t.Members = append(t.Members, playerName)
	m.playerTeam[key] = teamName
	delete(m.invites, key)
	if err := m.save(); err != nil {
		return "Failed to save: " + err.Error()
	}
	return ""
}

// Leave removes playerName from their team. Owners can't leave — they
// must disband instead, matching the PHP original.
func (m *Manager) Leave(playerName string) string {
	m.mu.Lock()
	defer m.mu.Unlock()

	teamName, ok := m.playerTeam[lower(playerName)]
	if !ok {
		return "You're not in a team."
	}
	t := m.teams[teamName]
	if t == nil {
		delete(m.playerTeam, lower(playerName))
		return "You're not in a team."
	}
	if lower(t.Owner) == lower(playerName) {
		return "Team owners can't leave - disband the team instead."
	}

	kept := t.Members[:0:0]
	for _, member := range t.Members {
		if lower(member) != lower(playerName) {
			kept = append(kept, member)
		}
	}
	t.Members = kept
	delete(m.playerTeam, lower(playerName))
	if err := m.save(); err != nil {
		return "Failed to save: " + err.Error()
	}
	return ""
}

// Kick removes target from owner's team. Owner-only; can't be used on the
// owner themself. Returns "" on success.
func (m *Manager) Kick(owner, target string) string {
	m.mu.Lock()
	defer m.mu.Unlock()

	teamName, ok := m.playerTeam[lower(owner)]
	if !ok {
		return "You don't own a team."
	}
	t := m.teams[teamName]
	if t == nil || lower(t.Owner) != lower(owner) {
		return "You don't own a team."
	}
	if lower(owner) == lower(target) {
		return "You can't kick yourself - disband the team instead."
	}

	var match string
	for _, member := range t.Members {
		if lower(member) == lower(target) {
			match = member
			break
		}
	}
	if match == "" {
		return target + " isn't in your team."
	}

	kept := t.Members[:0:0]
	for _, member := range t.Members {
		if lower(member) != lower(match) {
			kept = append(kept, member)
		}
	}
	t.Members = kept
	delete(m.playerTeam, lower(match))
	if err := m.save(); err != nil {
		return "Failed to save: " + err.Error()
	}
	return ""
}

// SetFriendlyFire sets owner's team friendly-fire flag. Returns "" on
// success.
func (m *Manager) SetFriendlyFire(owner string, enabled bool) string {
	m.mu.Lock()
	defer m.mu.Unlock()

	teamName, ok := m.playerTeam[lower(owner)]
	if !ok {
		return "You don't own a team."
	}
	t := m.teams[teamName]
	if t == nil || lower(t.Owner) != lower(owner) {
		return "You don't own a team."
	}
	t.FriendlyFire = enabled
	if err := m.save(); err != nil {
		return "Failed to save: " + err.Error()
	}
	return ""
}

// RecordKill increments killer's kill count on their current team, if any.
// No-op (not an error) if killer isn't in a team — matches the PHP
// original's TeamManager::recordKill, which silently returns.
func (m *Manager) RecordKill(killer string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	teamName, ok := m.playerTeam[lower(killer)]
	if !ok {
		return
	}
	t := m.teams[teamName]
	if t == nil {
		return
	}
	t.Kills[killer]++
	_ = m.save()
}

// RecordDeath increments victim's death count on their current team, if
// any. No-op if victim isn't in a team.
func (m *Manager) RecordDeath(victim string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	teamName, ok := m.playerTeam[lower(victim)]
	if !ok {
		return
	}
	t := m.teams[teamName]
	if t == nil {
		return
	}
	t.Deaths[victim]++
	_ = m.save()
}
