# The Effect vocabulary is closed, and hand-coded Features bind by Entry ID

A Feature's Effects use a closed vocabulary of triggers, conditions, frequencies and outcomes that the content package validates and the rules engine interprets; nothing in an Entry is an expression language or script. A word joins the vocabulary only when the engine can resolve it, in the same change. A Feature that the vocabulary can't express is flagged `requires_implementation`, declares no Effects or Activation, and the engine binds hand-written code to it by its Entry ID, behind the same effect interface the interpreter uses. We chose this over a general expression language in content (the Forge would write code the engine then trusts, against ADR 0001, and validation could no longer prove an Entry means something) and over partly declarative flagged Features (two sources of truth for one Feature).

## Consequences

- Renaming a flagged Feature's ID breaks its binding; the engine refuses to start a combat with a flagged Feature it has no code for, so the break is loud.
- Adding a vocabulary word touches the content package (validation) and the engine (interpretation) together; the Forge can only emit words already in it.
- A condition may join the vocabulary before the engine models what it tests, when the engine can still resolve it exactly: it holds for nothing that exists yet. `ranged_weapon` is the one such word; it is false while every weapon is a Melee weapon, which lets Sneak Attack's text be encoded whole. Outcomes, triggers and activation words get no such allowance.
- Level scaling (Sneak Attack dice, Rage Damage, Rage uses) isn't in the vocabulary yet: each Entry is the level-1 instance until character progression lands.
