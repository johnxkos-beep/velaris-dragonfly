package dfworlds

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	dfworld "github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/mcdb"
)

const defaultRoot = "worlds"

var (
	// ErrInvalidName is returned when a world name is empty or contains path
	// separators/traversal.
	ErrInvalidName = errors.New("invalid world name")
	// ErrWorldNotFound is returned when a requested world has not been loaded.
	ErrWorldNotFound = errors.New("world not found")
	// ErrWorldExists is returned when registering a duplicate world name.
	ErrWorldExists = errors.New("world already exists")
)

// Config controls how Manager loads additional worlds.
type Config struct {
	// Root is the folder containing one subdirectory per world. It defaults to
	// "worlds".
	Root string
	Log  *slog.Logger

	// Entities and Blocks should usually match the server's configured
	// registries so custom entities/blocks decode consistently across worlds.
	Entities dfworld.EntityRegistry
	Blocks   dfworld.BlockRegistry

	// Handler is applied to each loaded world. Leave nil to use Dragonfly's
	// NopHandler.
	Handler dfworld.Handler

	ReadOnly            bool
	SaveInterval        time.Duration
	ChunkUnloadInterval time.Duration
	RandomTickSpeed     int

	// Generator, when set, supplies the generator for a loaded world. Existing
	// chunks are read from disk; the generator is only used for missing chunks.
	Generator func(name string, dim dfworld.Dimension) dfworld.Generator

	// Configure can make final adjustments to the world.Config before the world
	// is opened. The Provider field is already set to the world's mcdb provider.
	Configure func(name string, conf dfworld.Config) dfworld.Config
}

// Manager owns a set of named Dragonfly worlds.
type Manager struct {
	mu      sync.RWMutex
	root    string
	conf    Config
	entries map[string]entry
}

type entry struct {
	name     string
	folder   string
	path     string
	world    *dfworld.World
	spawn    Spawn
	gameMode dfworld.GameMode
	owned    bool
}

// New creates an empty world manager.
func New(conf Config) *Manager {
	if conf.Root == "" {
		conf.Root = defaultRoot
	}
	if conf.Log == nil {
		conf.Log = slog.Default()
	}
	return &Manager{
		root:    conf.Root,
		conf:    conf,
		entries: make(map[string]entry),
	}
}

// LoadAll loads every child directory under the configured root. Worlds that
// are already loaded are skipped.
func (m *Manager) LoadAll() ([]string, error) {
	if err := os.MkdirAll(m.root, 0755); err != nil {
		return nil, fmt.Errorf("create DFWorlds root: %w", err)
	}
	children, err := os.ReadDir(m.root)
	if err != nil {
		return nil, fmt.Errorf("read DFWorlds root: %w", err)
	}

	var (
		loaded []string
		errs   []error
	)
	for _, child := range children {
		if !child.IsDir() {
			continue
		}
		w, err := m.Load(child.Name())
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if w != nil {
			loaded = append(loaded, child.Name())
		}
	}
	sort.Strings(loaded)
	return loaded, errors.Join(errs...)
}

// Load opens the named world from the manager root and returns it. Calling Load
// for an already-loaded world returns the existing world.
func (m *Manager) Load(name string) (*dfworld.World, error) {
	loaded, err := m.Open(Definition{Name: name})
	if err != nil {
		return nil, err
	}
	return loaded.World, nil
}

// Open opens a declared destination world from disk. Calling Open for an
// already-loaded destination returns the existing loaded world snapshot.
func (m *Manager) Open(def Definition) (LoadedWorld, error) {
	clean, folder, err := cleanDefinitionNames(def)
	if err != nil {
		return LoadedWorld{}, err
	}
	key := worldKey(clean)

	m.mu.RLock()
	if ent, ok := m.entries[key]; ok {
		m.mu.RUnlock()
		return loadedFromEntry(ent), nil
	}
	m.mu.RUnlock()

	m.mu.Lock()
	defer m.mu.Unlock()
	if ent, ok := m.entries[key]; ok {
		return loadedFromEntry(ent), nil
	}

	dim := def.Dimension
	if dim == nil {
		dim = dfworld.Overworld
	}

	path := filepath.Join(m.root, folder)
	log := m.conf.Log.With("world", clean)
	provider, err := (mcdb.Config{
		Log:    log,
		Blocks: m.conf.Blocks,
	}).Open(path)
	if err != nil {
		return LoadedWorld{}, fmt.Errorf("open world %q: %w", clean, err)
	}

	conf := dfworld.Config{
		Log:                 log,
		Dim:                 dim,
		Provider:            provider,
		Entities:            m.conf.Entities,
		Blocks:              m.conf.Blocks,
		ReadOnly:            m.conf.ReadOnly,
		SaveInterval:        m.conf.SaveInterval,
		ChunkUnloadInterval: m.conf.ChunkUnloadInterval,
		RandomTickSpeed:     m.conf.RandomTickSpeed,
	}
	if def.Generator != nil {
		conf.Generator = def.Generator(dim)
	} else if m.conf.Generator != nil {
		conf.Generator = m.conf.Generator(clean, dim)
	}
	if m.conf.Configure != nil {
		conf = m.conf.Configure(clean, conf)
	}
	if def.Configure != nil {
		conf = def.Configure(conf)
	}

	w := conf.New()
	handler := m.conf.Handler
	if def.Handler != nil {
		handler = def.Handler
	}
	if handler != nil {
		w.Handle(handler)
	}
	ent := entry{name: clean, folder: folder, path: path, world: w, owned: true}
	applyDefinitionMetadata(w, &ent, def)
	m.entries[key] = ent
	return loadedFromEntry(ent), nil
}

// OpenAll opens every declared destination in order. All definitions are
// attempted and any errors are joined.
func (m *Manager) OpenAll(defs ...Definition) ([]LoadedWorld, error) {
	loaded := make([]LoadedWorld, 0, len(defs))
	var errs []error
	for _, def := range defs {
		w, err := m.Open(def)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		loaded = append(loaded, w)
	}
	return loaded, errors.Join(errs...)
}

// Register adds an existing world to the manager without taking ownership of
// its lifecycle. Registered worlds are not closed by Manager.Close.
func (m *Manager) Register(name string, w *dfworld.World) error {
	_, err := m.RegisterWorld(Definition{Name: name}, w)
	return err
}

// RegisterWorld adds an existing world to the manager without taking ownership
// of its lifecycle. Registered worlds are not closed by Manager.Close.
func (m *Manager) RegisterWorld(def Definition, w *dfworld.World) (LoadedWorld, error) {
	if w == nil {
		return LoadedWorld{}, errors.New("register world: nil world")
	}
	clean, folder, err := cleanDefinitionNames(def)
	if err != nil {
		return LoadedWorld{}, err
	}
	key := worldKey(clean)

	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.entries[key]; ok {
		return LoadedWorld{}, fmt.Errorf("%w: %s", ErrWorldExists, clean)
	}
	ent := entry{name: clean, folder: folder, world: w}
	applyDefinitionMetadata(w, &ent, def)
	m.entries[key] = ent
	return loadedFromEntry(ent), nil
}

// World returns a loaded world by name.
func (m *Manager) World(name string) (*dfworld.World, bool) {
	clean, err := cleanName(name)
	if err != nil {
		return nil, false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	ent, ok := m.entries[worldKey(clean)]
	if !ok {
		return nil, false
	}
	return ent.world, true
}

// Destination returns a loaded destination snapshot by name.
func (m *Manager) Destination(name string) (LoadedWorld, bool) {
	clean, err := cleanName(name)
	if err != nil {
		return LoadedWorld{}, false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	ent, ok := m.entries[worldKey(clean)]
	if !ok {
		return LoadedWorld{}, false
	}
	return loadedFromEntry(ent), true
}

// MustDestination returns a loaded destination snapshot by name or an error.
func (m *Manager) MustDestination(name string) (LoadedWorld, error) {
	d, ok := m.Destination(name)
	if !ok {
		return LoadedWorld{}, fmt.Errorf("%w: %s", ErrWorldNotFound, name)
	}
	return d, nil
}

// Spawn returns the default travel spawn for a loaded destination.
func (m *Manager) Spawn(name string) (Spawn, bool) {
	d, ok := m.Destination(name)
	if !ok {
		return Spawn{}, false
	}
	return d.Spawn, true
}

// MustWorld returns a loaded world by name or an error.
func (m *Manager) MustWorld(name string) (*dfworld.World, error) {
	w, ok := m.World(name)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrWorldNotFound, name)
	}
	return w, nil
}

// Names returns all loaded world names in stable order.
func (m *Manager) Names() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	names := make([]string, 0, len(m.entries))
	for _, ent := range m.entries {
		names = append(names, ent.name)
	}
	sort.Strings(names)
	return names
}

// Destinations returns all loaded destination snapshots in stable name order.
func (m *Manager) Destinations() []LoadedWorld {
	m.mu.RLock()
	defer m.mu.RUnlock()

	destinations := make([]LoadedWorld, 0, len(m.entries))
	for _, ent := range m.entries {
		destinations = append(destinations, loadedFromEntry(ent))
	}
	sort.Slice(destinations, func(i, j int) bool {
		return destinations[i].Name < destinations[j].Name
	})
	return destinations
}

// Close closes every world loaded by this manager. Registered external worlds
// are left open.
func (m *Manager) Close() error {
	m.mu.Lock()
	entries := m.entries
	m.entries = make(map[string]entry)
	m.mu.Unlock()

	var errs []error
	for _, ent := range entries {
		if !ent.owned || ent.world == nil {
			continue
		}
		if err := ent.world.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close world %q: %w", ent.name, err))
		}
	}
	return errors.Join(errs...)
}

func cleanName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." || filepath.IsAbs(name) {
		return "", fmt.Errorf("%w: %q", ErrInvalidName, name)
	}
	if strings.ContainsAny(name, `/\`) || filepath.Clean(name) != name {
		return "", fmt.Errorf("%w: %q", ErrInvalidName, name)
	}
	return name, nil
}

func worldKey(name string) string {
	return strings.ToLower(name)
}

func cleanDefinitionNames(def Definition) (name, folder string, err error) {
	name, err = cleanName(def.Name)
	if err != nil {
		return "", "", err
	}
	folder = def.Folder
	if folder == "" {
		folder = name
	}
	folder, err = cleanName(folder)
	if err != nil {
		return "", "", err
	}
	return name, folder, nil
}

func applyDefinitionMetadata(w *dfworld.World, ent *entry, def Definition) {
	ent.spawn = SpawnFromWorld(w)
	if def.Spawn != nil {
		ent.spawn = *def.Spawn
		w.SetSpawn(cube.PosFromVec3(def.Spawn.Position))
	}

	ent.gameMode = w.DefaultGameMode()
	if def.GameMode != nil {
		ent.gameMode = def.GameMode
		w.SetDefaultGameMode(def.GameMode)
	}
}

func loadedFromEntry(ent entry) LoadedWorld {
	return LoadedWorld{
		Name:     ent.name,
		Folder:   ent.folder,
		Path:     ent.path,
		World:    ent.world,
		Spawn:    ent.spawn,
		GameMode: ent.gameMode,
		Owned:    ent.owned,
	}
}
