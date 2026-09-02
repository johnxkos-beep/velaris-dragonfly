package state

import dfworlds "velaris-dragonfly/worlds"

// Worlds is the shared DFWorlds manager for extra destination worlds
// (arenas, lobbies, minigame maps, etc.) beyond the server's built-in
// Overworld/Nether/End. Initialised in main() after the server itself is
// created — see main.go.
//
// NOTE: if `state` already declares a symbol named Worlds or WorldRouter,
// rename these (and their main.go/commands references) to something else —
// this file was added blind, without seeing the rest of the package.
var Worlds *dfworlds.Manager

// WorldRouter is the shared DFWorlds router commands use to send players
// between destinations registered on Worlds.
var WorldRouter *dfworlds.Router
