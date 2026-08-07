// Package teams ports HopliteLegendary's core/teams system: create/join/
// leave/kick/disband, invites, friendly-fire toggle, per-member kill/death
// tracking, colored nametags, and a team-only chat mode.
//
// Not ported from the original (matches its own scope, not a regression):
// nothing — this is a complete 1:1 port of TeamManager.php's logic.
//
// This has not been run against a live Dragonfly server. See legendary's
// README.md in this same repo for the general "treat first boot as a
// shakeout" caveat — it applies here too.
package teams

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
)

// MaxMembers is the maximum number of players (including the owner) a
// single team can have, matching TeamManager::MAX_MEMBERS.
const MaxMembers = 5

// Team is one team's persisted state.
type Team struct {
	Name         string         `json:"name"`
	Owner        string         `json:"owner"` // exact-case display name
	Color        string         `json:"color"` // Minecraft color code, e.g. "§c"
	FriendlyFire bool           `json:"friendlyFire"`
	Members      []string       `json:"members"` // exact-case display names, owner included
	Kills        map[string]int `json:"kills"`    // exact-case member name -> kill count
	Deaths       map[string]int `json:"deaths"`   // exact-case member name -> death count
}

// Manager tracks every team, which team each player belongs to, and
// pending invites, persisting all of it to a single JSON file.
type Manager struct {
	mu   sync.RWMutex
	path string

	teams      map[string]*Team    // team name -> team
	playerTeam map[string]string   // lowercase player name -> team name
	invites    map[string][]string // lowercase player name -> team names inviting them
}

type persisted struct {
	Teams      map[string]*Team    `json:"teams"`
	PlayerTeam map[string]string   `json:"playerTeam"`
	Invites    map[string][]string `json:"invites"`
}

// NewManager loads (or creates) the teams file at path.
func NewManager(path string) (*Manager, error) {
	m := &Manager{
		path:       path,
		teams:      map[string]*Team{},
		playerTeam: map[string]string{},
		invites:    map[string][]string{},
	}
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return m, nil
	}
	if err != nil {
		return nil, fmt.Errorf("teams: read file: %w", err)
	}
	var p persisted
	if err := json.Unmarshal(b, &p); err != nil {
		return nil, fmt.Errorf("teams: parse file: %w", err)
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
	return m, nil
}

func (m *Manager) save() error {
	p := persisted{Teams: m.teams, PlayerTeam: m.playerTeam, Invites: m.invites}
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(m.path, b, 0644)
}

// ---------------------------------------------------------------------
// Read helpers
// ---------------------------------------------------------------------

// Team returns the team with the given exact name, or nil.
func (m *Manager) Team(name string) *Team {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.teams[name]
}

// TeamOfPlayer returns the team the given player (by display name) belongs
// to, or nil if they're not in one.
func (m *Manager) TeamOfPlayer(playerName string) *Team {
	m.mu.RLock()
	defer m.mu.RUnlock()
	name, ok := m.playerTeam[strings.ToLower(playerName)]
	if !ok {
		return nil
	}
	return m.teams[name]
}

// IsOwner reports whether playerName owns the given team.
func (m *Manager) IsOwner(playerName, teamName string) bool {
	t := m.Team(teamName)
	return t != nil && strings.EqualFold(t.Owner, playerName)
}

// Invites returns the team names that have invited playerName.
func (m *Manager) Invites(playerName string) []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return append([]string(nil), m.invites[strings.ToLower(playerName)]...)
}

// ---------------------------------------------------------------------
// Mutations — each mirrors its TeamManager.php counterpart, returning ""
// on success or a user-facing error message otherwise.
// ---------------------------------------------------------------------

// CreateTeam creates a new team owned by owner.
func (m *Manager) CreateTeam(owner, teamName, color string) string {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.playerTeam[strings.ToLower(owner)]; ok {
		return "You're already in a team."
	}
	if _, ok := m.teams[teamName]; ok {
		return "That team name is already taken."
	}
	if trimmed := strings.TrimSpace(teamName); trimmed == "" || len(teamName) > 16 {
		return "Team names must be 1-16 characters."
	}

	m.teams[teamName] = &Team{
		Name:    teamName,
		Owner:   owner,
		Color:   color,
		Members: []string{owner},
		Kills:   map[string]int{},
		Deaths:  map[string]int{},
	}
	m.playerTeam[strings.ToLower(owner)] = teamName
	if err := m.save(); err != nil {
		return "Failed to save: " + err.Error()
	}
	return ""
}

// Disband deletes owner's team. owner must be the team's owner.
func (m *Manager) Disband(owner string) string {
	m.mu.Lock()
	defer m.mu.Unlock()

	t := m.teamOfPlayerLocked(owner)
	if t == nil || !strings.EqualFold(t.Owner, owner) {
		return "You don't own a team."
	}
	for _, member := range t.Members {
		delete(m.playerTeam, strings.ToLower(member))
	}
	delete(m.teams, t.Name)
	if err := m.save(); err != nil {
		return "Failed to save: " + err.Error()
	}
	return ""
}

// Invite adds target to owner's team's pending invite list.
func (m *Manager) Invite(owner, target string) string {
	m.mu.Lock()
	defer m.mu.Unlock()

	t := m.teamOfPlayerLocked(owner)
	if t == nil || !strings.EqualFold(t.Owner, owner) {
		return "You don't own a team."
	}
	if len(t.Members) >= MaxMembers {
		return fmt.Sprintf("Your team is already full (%d members).", MaxMembers)
	}
	if m.teamOfPlayerLocked(target) != nil {
		return target + " is already in a team."
	}

	key := strings.ToLower(target)
	for _, existing := range m.invites[key] {
		if existing == t.Name {
			if err := m.save(); err != nil {
				return "Failed to save: " + err.Error()
			}
			return ""
		}
	}
	m.invites[key] = append(m.invites[key], t.Name)
	if err := m.save(); err != nil {
		return "Failed to save: " + err.Error()
	}
	return ""
}

// AcceptInvite adds playerName to teamName, provided they were invited.
func (m *Manager) AcceptInvite(playerName, teamName string) string {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := strings.ToLower(playerName)
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
	if m.teamOfPlayerLocked(playerName) != nil {
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

// Leave removes playerName from their team. The owner can't leave this way
// (they must Disband instead), matching the original.
func (m *Manager) Leave(playerName string) string {
	m.mu.Lock()
	defer m.mu.Unlock()

	t := m.teamOfPlayerLocked(playerName)
	if t == nil {
		return "You're not in a team."
	}
	if strings.EqualFold(t.Owner, playerName) {
		return "Team owners can't leave - disband the team instead."
	}
	t.Members = removeFold(t.Members, playerName)
	delete(m.playerTeam, strings.ToLower(playerName))
	if err := m.save(); err != nil {
		return "Failed to save: " + err.Error()
	}
	return ""
}

// Kick removes target from owner's team. owner must be the team's owner and
// can't kick themselves.
func (m *Manager) Kick(owner, target string) string {
	m.mu.Lock()
	defer m.mu.Unlock()

	t := m.teamOfPlayerLocked(owner)
	if t == nil || !strings.EqualFold(t.Owner, owner) {
		return "You don't own a team."
	}
	if strings.EqualFold(owner, target) {
		return "You can't kick yourself - disband the team instead."
	}

	var match string
	for _, member := range t.Members {
		if strings.EqualFold(member, target) {
			match = member
			break
		}
	}
	if match == "" {
		return target + " isn't in your team."
	}

	t.Members = removeFold(t.Members, match)
	delete(m.playerTeam, strings.ToLower(match))
	if err := m.save(); err != nil {
		return "Failed to save: " + err.Error()
	}
	return ""
}

// SetFriendlyFire toggles friendly fire for owner's team.
func (m *Manager) SetFriendlyFire(owner string, enabled bool) string {
	m.mu.Lock()
	defer m.mu.Unlock()

	t := m.teamOfPlayerLocked(owner)
	if t == nil || !strings.EqualFold(t.Owner, owner) {
		return "You don't own a team."
	}
	t.FriendlyFire = enabled
	if err := m.save(); err != nil {
		return "Failed to save: " + err.Error()
	}
	return ""
}

// RecordKill increments killer's kill count on their team, if they're in
// one. No-op (not an error) if they aren't.
func (m *Manager) RecordKill(killer string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t := m.teamOfPlayerLocked(killer)
	if t == nil {
		return
	}
	t.Kills[killer]++
	_ = m.save()
}

// RecordDeath increments victim's death count on their team, if they're in
// one. No-op (not an error) if they aren't.
func (m *Manager) RecordDeath(victim string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t := m.teamOfPlayerLocked(victim)
	if t == nil {
		return
	}
	t.Deaths[victim]++
	_ = m.save()
}

// teamOfPlayerLocked is TeamOfPlayer without taking the lock itself, for
// use from methods that already hold it.
func (m *Manager) teamOfPlayerLocked(playerName string) *Team {
	name, ok := m.playerTeam[strings.ToLower(playerName)]
	if !ok {
		return nil
	}
	return m.teams[name]
}

func removeFold(list []string, target string) []string {
	out := make([]string, 0, len(list))
	for _, v := range list {
		if !strings.EqualFold(v, target) {
			out = append(out, v)
		}
	}
	return out
}
