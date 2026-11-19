# dndboard

A cooperative online tabletop game that plays an official D&D adventure on a grid board, with the computer acting as the narrator and D&D 2024 rules applied exactly.

## Language

### Rules and sources

**Ruleset**:
The D&D 2024 revision of fifth edition, with SRD 5.2 as the bundled baseline.
_Avoid_: 5.5, One D&D, 5e, D&D Next

**Adventure Book**:
An official D&D adventure the user supplies as a PDF, in English or Portuguese. It is the canonical source for a Campaign's story, Scenes and Encounters.
_Avoid_: module, PDF, scenario

**Compendium**:
All the standard D&D 2024 material (character options, spells, monsters, items) processed once and validated, which every Adventure references. SRD 5.2 is its bundled baseline; the rest comes from the user's own core books.
_Avoid_: database, library, core, rules data

**Adventure**:
The structured, reviewed result of importing an Adventure Book: its Scenes, Encounters, NPCs, Transitions and treasure, translated to Portuguese when needed.
_Avoid_: imported book, campaign data

**Forge**:
The separate tool that turns PDFs into the Compendium and Adventures, including their review.
_Avoid_: importer, parser, pipeline, converter

### Roles

**Narrator**:
The computer in the role of the Dungeon Master: it describes the world, plays NPCs and monsters, and asks for checks, but never decides mechanical outcomes.
_Avoid_: DM, Mestre, AI, GM

**Player**:
A human taking part in a Campaign, controlling one or more Player Characters.
_Avoid_: user, client

**Player Character**:
A character built and advanced by the full D&D 2024 character-creation rules and controlled by a Player.
_Avoid_: hero, unit, PC (in prose)

**Party**:
The Player Characters adventuring together in a Campaign.
_Avoid_: group, team

### Play

**Campaign**:
One Party's playthrough of one Adventure Book, from start to finish.
_Avoid_: game, session, run

**Scene**:
A part of the Adventure Book relevant enough to play on a Board. Parts of the book that aren't relevant to the Campaign are skipped, not played.
_Avoid_: room, level, chapter

**Transition**:
A short narrated summary standing in for a part of the Adventure Book that is skipped because it isn't relevant to the Campaign.
_Avoid_: cutscene, skip

**Interlude**:
A pause between Scenes, without a Board, where the Party rests, shops and levels up.
_Avoid_: downtime, menu, hub, camp

**Board**:
The grid map a Scene is played on, generated from the Adventure Book's description of that place.
_Avoid_: map, battlemap, dungeon

**Encounter**:
A challenge the Adventure Book places in a Scene: monsters, a trap, a hazard or an NPC interaction.
_Avoid_: fight, combat, event

### Rules engine

**Intent**:
What a Player (or a monster's tactics) asks the active creature to do, such as move to a square or attack a target. The rules engine validates it and either resolves it into events or rejects it with a reason.
_Avoid_: command, order, input

**Legal Actions**:
The active creature's choices this turn, precomputed by the server: reachable squares with their movement cost, valid targets, areas of effect. The client only renders them.
_Avoid_: options, moves (for the whole set)

**Initiative**:
The order of turns in a combat, set once when everyone rolls a Dexterity check and then repeated every round. Ties keep the order the caller listed the creatures, standing in for the GM's (and players') choice.
_Avoid_: turn order (as the rule name)

**Action Economy**:
What a creature may spend on its turn: its movement (splittable around anything else), one action, one Bonus Action, and one Reaction that is regained at the start of its own turn.
_Avoid_: AP, action points

### Content format

**Entry**:
One piece of Compendium or Adventure data in the content format, such as a weapon, a stat block or a Feature. Entries are pure data or Effects, validated against the schema, and reference each other by ID.
_Avoid_: record, row, asset

**Feature**:
An Entry for a class feature, feat or trait (Sneak Attack, Rage, Savage Attacker) that a stat block lists by ID. Its behavior is a list of Effects, optionally behind an Activation, or else it requires implementation.
_Avoid_: ability, perk, trait (as the Entry kind)

**Effect**:
One declarative behavior of a Feature: a trigger (such as hitting with an attack), conditions that must all hold, a frequency (always, or once per turn) and an outcome (such as extra damage or Resistance). The rules engine interprets it; the vocabulary is closed and grows with the engine.
_Avoid_: rule, script, modifier

**Activation**:
What makes a Feature's Effects apply only while it is on: its cost (a Bonus Action), uses, duration, what extends it and what ends it early, as for Rage.
_Avoid_: toggle, buff

**Requires implementation**:
The flag on a Feature whose behavior the Effect vocabulary can't express. Such a Feature declares no Effects; the rules engine runs hand-written code registered under the Feature's ID, behind the same interface as interpreted Effects, and refuses to start a combat with a flagged Feature it has no code for.
_Avoid_: custom, scripted, special-case

**Review status**:
Whether an Entry is `draft` or `approved`. Only approved Entries load into the game; an approved Entry may only reference approved Entries.
_Avoid_: state, flag, verified
