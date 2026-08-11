# Demon King boss — ported from the "all bosses" add-on

This is a from-scratch Go reimplementation of the Demon King (Lord Demon /
"enmu") boss for velaris-dragonfly, built by reading the add-on's behaviour
pack JSON directly (entity component values, damage numbers from the
animation timelines, timer lengths). No item drops, no weapon-swap
mechanics — spawn egg only, per your request.

**Read `bosses/demonking/entity.go`'s top comment first** — it lists exactly
what was ported (two 100 HP phases, melee, the two real AoE abilities with
their real damage numbers, aggro radii) and what was deliberately left out
(the eye-item wake-up ritual, all loot).

## Important: this needs a build pass on your end

I don't have a Go toolchain or network access in this sandbox, so none of
this has been compiled. Two things make that riskier than usual here:

1. **Dragonfly has no built-in mob AI/pathfinding** (confirmed against the
   current docs) — unlike PocketMine-MP, there's no vanilla navigation
   system to hook into. The boss moves in a straight line toward its
   target ("hover" style) rather than pathing around obstacles. That's a
   deliberate, documented simplification, not a bug.
2. Dragonfly recently moved to a lower-level, transaction-based entity API
   (`world.Tx`/`EntityHandle`/`EntityType`) that I verified against the
   current source and docs, but a couple of specific method names — how a
   custom entity marks itself damageable by a player's melee attack, and
   the exact `world.Tx` sound/particle call signatures — I could not
   confirm against a real build. Both spots are flagged with comments in
   the code (search for "UNVERIFIED" in `entity.go` and `spawnegg.go`).

**Fastest path:** run `go build ./...` on your VPS, and paste me whatever
errors come back. Those two spots are the most likely source, and they're
narrow, mechanical fixes — the fight logic itself (phases, HP, damage
numbers, timers) won't need to change.

## Wiring it into main.go

Three small edits, matching how `legendaryweapons` is already wired in:

```go
import (
    // ...
    "velaris-dragonfly/bosses/demonking"
)
```

Right next to the existing `legendaryweapons.Register()` call (around line 263):

```go
legendaryweapons.Register()
demonking.Register()
```

Right after `conf, err := userConf.Config(log)` and before `conf.Generator = ...`:

```go
conf.Entities = demonking.EntityRegistry()
```

That's it — `srv := conf.New()` and `state.Server = srv` (both already in
your main.go) are all `demonking` needs; it reads the online player list
through the existing `state.Server` global.

## Getting the model to render (resource pack)

You don't need to build a new resource pack. The Go code spawns the boss
under the identifier `tnt:lord_demon` — the exact identifier your uploaded
`all-bosses-texture.mcaddon` already uses for its client-side model/
animations (`all bosses RE/entity/demon_king/bss/lord_demon.json`). Drop
that resource pack's contents into wherever this server serves its resource
packs from (check `resources/` per the Dragonfly template, or however
Pterodactyl's mounted it) and the client will render it automatically —
Bedrock matches entities to resource packs by identifier, nothing else
needed. You can safely trim that RP down to just the `demon_king` entity/
model/animation/texture files if you want to drop the other bosses' assets.

## Spawning it in-game

Give yourself the spawn egg in creative/via command once it's built:

```
/give @s tnt:lord_demon_spawn_egg
```

Right-click a block to summon the boss there, already awake.

## Next steps

Enderman Boss and Shadow Golem weren't touched — say the word once Demon
King is confirmed working and I'll do the next one the same way.
