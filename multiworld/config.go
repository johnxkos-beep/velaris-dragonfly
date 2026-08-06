package multiworld

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
)

// WorldType mirrors the generator choices czechpmdevs/multiworld offered.
// "overworld" and "nether" delegate to your existing worldgen package
// rather than reimplementing vanilla terrain gen a second time.
type WorldType string

const (
	TypeOverworld WorldType = "overworld"
	TypeNether    WorldType = "nether"
	TypeVoid      WorldType = "void"
	TypeSkyBlock  WorldType = "skyblock"
	TypeEnd       WorldType = "end" // end-stone island, see generators.go's note on this
)

// Entry is one registered world's persisted metadata.
type Entry struct {
	Name   string    `json:"name"`
	Type   WorldType `json:"type"`
	Seed   int64     `json:"seed"`
	Folder string    `json:"folder"` // subdirectory under Root
}

// registry is the on-disk list of every world ever created through /mw,
// independent of which ones are currently loaded in memory (see manager.go
// for the loaded-worlds tracking).
type registry struct {
	mu     sync.Mutex
	path   string
	Worlds map[string]*Entry `json:"worlds"`
}

var reg *registry

// Root is the folder each world's save data lives under, as a subfolder per
// world (Root/<world.Folder>/), created next to the server binary — same
// pattern as schematics.Dir, since Dragonfly has no plugin_data equivalent
// either.
var Root = "worlds"

// InitRegistry loads (or creates) the world registry from path, and ensures
// Root exists. Call this once in main(), before registering /mw.
func InitRegistry(path string) error {
	if err := os.MkdirAll(Root, 0755); err != nil {
		return err
	}
	reg = &registry{path: path, Worlds: map[string]*Entry{}}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return reg.save()
		}
		return err
	}
	return json.Unmarshal(data, reg)
}

func (r *registry) save() error {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(r.path, data, 0644)
}

// MarshalJSON/UnmarshalJSON are needed because registry embeds a
// sync.Mutex, which json can't (and shouldn't) touch directly.
func (r *registry) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Worlds map[string]*Entry `json:"worlds"`
	}{Worlds: r.Worlds})
}

func (r *registry) UnmarshalJSON(data []byte) error {
	var aux struct {
		Worlds map[string]*Entry `json:"worlds"`
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	if aux.Worlds == nil {
		aux.Worlds = map[string]*Entry{}
	}
	r.Worlds = aux.Worlds
	return nil
}

func addEntry(e *Entry) error {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	if _, exists := reg.Worlds[e.Name]; exists {
		return fmt.Errorf("a world named %q already exists", e.Name)
	}
	reg.Worlds[e.Name] = e
	return reg.save()
}

func removeEntry(name string) error {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	if _, exists := reg.Worlds[name]; !exists {
		return fmt.Errorf("no world named %q", name)
	}
	delete(reg.Worlds, name)
	return reg.save()
}

func renameEntry(oldName, newName string) error {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	e, exists := reg.Worlds[oldName]
	if !exists {
		return fmt.Errorf("no world named %q", oldName)
	}
	if _, taken := reg.Worlds[newName]; taken {
		return fmt.Errorf("a world named %q already exists", newName)
	}
	delete(reg.Worlds, oldName)
	e.Name = newName
	reg.Worlds[newName] = e
	return reg.save()
}

func getEntry(name string) (*Entry, bool) {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	e, ok := reg.Worlds[name]
	return e, ok
}

func allEntries() []*Entry {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	out := make([]*Entry, 0, len(reg.Worlds))
	for _, e := range reg.Worlds {
		out = append(out, e)
	}
	return out
}
