// Package dfworlds owns Dragonfly-native loading and switching between extra
// worlds.
//
// Dragonfly exposes worlds as independent *world.World values and its session
// code already follows an entity handle when it changes worlds. This package
// builds on that path directly: named destination loading, spawn/default
// gamemode metadata, explicit shutdown, and player travel helpers that move
// entity handles with world.Tx.RemoveEntity and world.Tx.AddEntity.
//
// No packet bridge is needed for normal same-server DFWorlds travel. The
// Router type is the high-level API intended for command, item, portal, or NPC
// code. Transaction callbacks use its Tx methods, while off-owner code uses its
// context-aware handle methods. Manager remains available when boot code needs
// direct lifecycle control.
package dfworlds
