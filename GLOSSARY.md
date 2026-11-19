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

**Standard Actions**:
The actions every creature has on its turn. Implemented so far: Attack, Dash, Disengage, Dodge, Help, Hide, Ready, Search and Utilize (Magic, Study and Influence come with spells and social play). Each spends the creature's one action.
_Avoid_: basic actions, combat actions

**Readied action**:
A response a creature holds after taking the Ready action, released with its Reaction when its trigger occurs. It lapses at the start of the creature's next turn.
_Avoid_: delayed action, overwatch

**Board object**:
A clickable thing placed on a Board (a door, chest or lever) that a creature within 5 feet uses with the Utilize action.
_Avoid_: prop, interactable, item

**Hidden**:
The Invisible Condition a creature gains by succeeding at the Hide action. It ends when the creature attacks or an enemy finds it with the Search action. Enemies cannot choose a hidden creature as a target.
_Avoid_: stealthed, concealed

**Effect**:
Something the rules engine applies to a creature outside its own turn's Intents, such as a trap, a spell or a ruling: inflicting or clearing a condition, damage, healing, a saving throw or an ability check. Applied through the game-session seam like an Intent, and answered with events. The declarative effects of spells and class features (#8) compile down to these.
_Avoid_: trigger, hit

**Condition**:
One of the 15 2024 states (Blinded to Unconscious) that changes a creature's rolls, Speed or actions until it ends. Some imply others (Unconscious includes Incapacitated and Prone). Exhaustion is cumulative, by level.
_Avoid_: status, debuff, status effect

**Dying**:
A Player Character at 0 Hit Points that is not Stable: Unconscious, making a Death Saving Throw at the start of each of its turns and suffering a failure for each hit it takes. Monsters skip this and die at 0 Hit Points. Stable creatures stay at 0 Hit Points without rolling.
_Avoid_: bleeding out, downed (as the rule name)

**Reaction Prompt**:
The pause in which the game asks a Player Character's owner whether it uses its Reaction (today: an Opportunity Attack as a hostile creature leaves its reach). The game waits until the owner answers or the Campaign's reaction timeout passes, and a timeout means no reaction.
_Avoid_: popup, confirmation

**Reaction Rule**:
A per-creature setting for how a Reaction is decided: always react, never react, or ask (the Reaction Prompt). A creature with no setting reacts always, which is how monsters behave; a Player Character is set to ask unless its owner chose always or never. The setting of a monster is never ask.
_Avoid_: auto-reaction, preference

### Content format

**Entry**:
One piece of Compendium or Adventure data in the content format, such as a weapon or a stat block. Entries are pure data, validated against the schema, and reference each other by ID.
_Avoid_: record, row, asset

**Review status**:
Whether an Entry is `draft` or `approved`. Only approved Entries load into the game; an approved Entry may only reference approved Entries.
_Avoid_: state, flag, verified
