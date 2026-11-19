package session

import (
	"slices"
	"strings"

	"github.com/mvgv/dndboard/server/board"
)

// own reports whether the creature itself carries the condition.
func (c *creature) own(k Condition) bool {
	_, ok := c.conds[k]
	return ok
}

// has reports whether a condition affects the creature, counting the ones
// others imply (SRD "Rules Glossary"): Paralyzed, Petrified, Stunned and
// Unconscious each include Incapacitated, Unconscious includes Prone, and
// Petrified gives Immunity to Poisoned.
func (c *creature) has(k Condition) bool {
	if slices.Contains(c.ConditionImmunities, k) {
		return false
	}
	switch k {
	case CondExhaustion:
		return c.exhaustion > 0
	case CondIncapacitated:
		return c.own(k) || c.own(CondParalyzed) || c.own(CondPetrified) ||
			c.own(CondStunned) || c.own(CondUnconscious)
	case CondProne:
		return c.own(k) || c.own(CondUnconscious)
	case CondPoisoned:
		return c.own(k) && !c.own(CondPetrified)
	}
	return c.own(k)
}

func (c *creature) conditions() []Condition {
	var out []Condition
	for _, k := range AllConditions {
		if c.has(k) {
			out = append(out, k)
		}
	}
	slices.Sort(out)
	return out
}

// speed is the creature's Speed now: 0 while Grappled, Paralyzed, Petrified,
// Restrained or Unconscious, and reduced by 5 feet per Exhaustion level.
func (c *creature) speed() int {
	for _, k := range []Condition{CondGrappled, CondParalyzed, CondPetrified, CondRestrained, CondUnconscious} {
		if c.has(k) {
			return 0
		}
	}
	return max(c.SpeedFt-5*c.exhaustion, 0)
}

// d20Penalty is what Exhaustion subtracts from every D20 Test.
func (c *creature) d20Penalty() int { return 2 * c.exhaustion }

func (c *creature) incapacitated() bool { return c.has(CondIncapacitated) }

// defeated: no longer able to fight on, for deciding when a combat is over.
func (c *creature) defeated() bool { return c.out() || c.down() }

func (c *creature) dying() bool { return c.PlayerCharacter && c.down() && !c.dead && !c.stable }

// rollD20 rolls a d20 with Advantage or Disadvantage; both cancel out.
func (s *Session) rollD20(adv, dis bool) (int, RollMode) {
	switch {
	case adv && !dis:
		return max(s.dice.Roll(20), s.dice.Roll(20)), Advantage
	case dis && !adv:
		return min(s.dice.Roll(20), s.dice.Roll(20)), Disadvantage
	}
	return s.dice.Roll(20), Normal
}

// checkDisadvantage reports Disadvantage on ability checks of an ability check (SRD "Rules Glossary":
// Poisoned and Frightened give Disadvantage on ability checks; Frightened's
// line of sight to the source is not modeled, so the source is always in sight).
func (c *creature) checkDisadvantage() bool {
	return c.has(CondPoisoned) || c.has(CondFrightened)
}

// adjustDamage applies Immunity, then Resistance, then Vulnerability, each once
// (SRD "Playing the Game", Resistance and Vulnerability). Petrified gives
// Resistance to all damage.
func (c *creature) adjustDamage(raw int, dtype string) (int, string) {
	match := func(types []string) bool {
		return slices.ContainsFunc(types, func(t string) bool { return strings.EqualFold(t, dtype) })
	}
	if match(c.Immunities) {
		return 0, "immunity"
	}
	amount, adj := raw, ""
	if match(c.Resistances) || c.has(CondPetrified) {
		amount /= 2
		adj = "resistance"
	}
	if match(c.Vulnerabilities) {
		amount *= 2
		if adj != "" {
			adj += "+"
		}
		adj += "vulnerability"
	}
	return amount, adj
}

// attackMode works out Advantage and Disadvantage on c's attack roll against t
// (SRD "Rules Glossary", conditions).
func attackMode(c, t *creature) (adv, dis bool) {
	near := board.Distance(c.Pos, t.Pos)*board.SquareFeet <= 5
	// The attacker's own conditions.
	adv = c.has(CondInvisible)
	dis = c.has(CondBlinded) || c.has(CondPoisoned) || c.has(CondProne) ||
		c.has(CondRestrained) || c.has(CondFrightened)
	if src, ok := c.conds[CondGrappled]; ok && c.has(CondGrappled) && src != t.ID {
		dis = true
	}
	// The target's conditions.
	for _, k := range []Condition{CondBlinded, CondParalyzed, CondPetrified, CondRestrained, CondStunned, CondUnconscious} {
		if t.has(k) {
			adv = true
		}
	}
	if t.has(CondInvisible) {
		dis = true
	}
	if t.has(CondProne) {
		if near {
			adv = true
		} else {
			dis = true
		}
	}
	return adv, dis
}

// autoCrit: a hit within 5 feet of a Paralyzed or Unconscious target is a
// Critical Hit.
func autoCrit(c, t *creature) bool {
	return board.Distance(c.Pos, t.Pos)*board.SquareFeet <= 5 &&
		(t.has(CondParalyzed) || t.has(CondUnconscious))
}

func (s *Session) target(id ID) (*creature, error) {
	t, ok := s.creatures[id]
	switch {
	case !ok:
		return nil, reject(InvalidTarget, "no creature %q", id)
	case t.dead:
		return nil, reject(InvalidTarget, "%s is dead", id)
	}
	return t, nil
}

// Apply resolves an Effect on the session (traps, spells, rulings). An illegal
// effect returns a *Rejection and changes nothing.
func (s *Session) Apply(e Effect) ([]Event, error) {
	if s.over {
		return nil, reject(CombatOver, "combat is over")
	}
	if s.pending != nil {
		return nil, reject(AwaitingReaction, "waiting for %s to answer a reaction prompt", s.pending.asked)
	}
	var out []Event
	switch e := e.(type) {
	case InflictCondition:
		t, err := s.target(e.Target)
		if err != nil {
			return nil, err
		}
		if e.Condition == CondGrappled || e.Condition == CondCharmed {
			if _, err := s.target(e.Source); err != nil {
				return nil, reject(InvalidTarget, "%s needs a source creature", e.Condition)
			}
		}
		out = s.inflict(t, e.Condition, e.Source)
	case ClearCondition:
		t, err := s.target(e.Target)
		if err != nil {
			return nil, err
		}
		out = s.clear(t, e.Condition)
	case InflictDamage:
		t, err := s.target(e.Target)
		if err != nil {
			return nil, err
		}
		out = s.applyDamage(t, DamageDealt{Target: t.ID, Raw: max(e.Amount, 0), Type: e.Type}, e.Critical)
	case Heal:
		t, err := s.target(e.Target)
		if err != nil {
			return nil, err
		}
		out = s.heal(t, e.Amount)
	case SavingThrow:
		t, err := s.target(e.Target)
		if err != nil {
			return nil, err
		}
		out = []Event{s.save(t, e)}
	case AbilityCheck:
		t, err := s.target(e.Target)
		if err != nil {
			return nil, err
		}
		out = []Event{s.check(t, e.Ability, t.CheckMods[e.Ability], e.DC, e.NeedsSight, e.NeedsHearing)}
	default:
		return nil, reject(NoSuchOption, "unknown effect %T", e)
	}
	out = s.settle(out)
	s.log = append(s.log, out...)
	return out, nil
}

// settle ends grapples whose grappler is gone and finishes the combat if a
// side has been defeated.
func (s *Session) settle(out []Event) []Event {
	for _, id := range s.order {
		c := s.creatures[id]
		src, ok := c.conds[CondGrappled]
		if !ok {
			continue
		}
		// SRD "Rules Glossary", Grappling: the condition ends if the grappler is
		// Incapacitated. A dead or departed grappler can't hold on either. The
		// grapple's range is not modeled.
		if g := s.creatures[src]; g == nil || g.out() || g.incapacitated() {
			out = append(out, s.clear(c, CondGrappled)...)
		}
	}
	return s.finishIfOver(out)
}

func (s *Session) inflict(t *creature, k Condition, src ID) []Event {
	if slices.Contains(t.ConditionImmunities, k) || (k == CondPoisoned && t.own(CondPetrified)) {
		return []Event{ConditionResisted{Creature: t.ID, Condition: k}}
	}
	if k == CondExhaustion {
		t.exhaustion++
		out := []Event{ConditionApplied{Creature: t.ID, Condition: k, Level: t.exhaustion}}
		if t.exhaustion >= 6 { // SRD "Rules Glossary", Exhaustion: dead at level 6
			out = append(out, s.die(t)...)
		}
		return out
	}
	if t.own(k) {
		return nil
	}
	t.conds[k] = src
	return []Event{ConditionApplied{Creature: t.ID, Condition: k, Source: src}}
}

func (s *Session) clear(t *creature, k Condition) []Event {
	if k == CondExhaustion {
		if t.exhaustion == 0 {
			return nil
		}
		t.exhaustion--
		return []Event{ConditionRemoved{Creature: t.ID, Condition: k, Level: t.exhaustion}}
	}
	if !t.own(k) {
		return nil
	}
	delete(t.conds, k)
	out := []Event{ConditionRemoved{Creature: t.ID, Condition: k}}
	if k == CondUnconscious { // SRD "Rules Glossary", Unconscious: remains Prone
		out = append(out, s.inflict(t, CondProne, "")...)
	}
	return out
}

// applyDamage applies Resistance, Vulnerability and Immunity to d.Raw, then the
// damage itself and what follows from it (SRD "Playing the Game", Dropping to 0
// Hit Points). d carries the dice details of an attack, if any.
func (s *Session) applyDamage(t *creature, d DamageDealt, crit bool) []Event {
	d.Amount, d.Adjustment = t.adjustDamage(d.Raw, d.Type)
	before := t.hp
	t.hp = max(before-d.Amount, 0)
	d.HPAfter = t.hp
	out := []Event{d}
	switch {
	case d.Amount == 0:
	case before > 0 && t.hp == 0:
		out = append(out, CreatureDowned{Creature: t.ID})
		// Monsters die at 0; a character dies if the damage left over equals or
		// exceeds their Hit Point maximum (Massive Damage).
		if !t.PlayerCharacter || d.Amount-before >= t.MaxHP {
			out = append(out, s.die(t)...)
		} else {
			t.deathSucc, t.deathFail, t.stable = 0, 0, false
			out = append(out, s.inflict(t, CondUnconscious, "")...)
		}
	case before == 0 && t.PlayerCharacter:
		// Damage at 0 Hit Points: a failure, two on a Critical Hit; death if it
		// equals or exceeds the Hit Point maximum. A Stable creature starts
		// making Death Saving Throws again.
		t.stable = false
		if d.Amount >= t.MaxHP {
			out = append(out, s.die(t)...)
			break
		}
		fails := 1
		if crit {
			fails = 2
		}
		t.deathFail += fails
		out = append(out, DeathSaveFailed{Creature: t.ID, Successes: t.deathSucc, Failures: t.deathFail})
		if t.deathFail >= 3 {
			out = append(out, s.die(t)...)
		}
	}
	return s.settle(out)
}

func (s *Session) die(t *creature) []Event {
	if t.dead {
		return nil
	}
	t.dead = true
	return []Event{CreatureDied{Creature: t.ID}}
}

// heal restores Hit Points, capped at the maximum. Regaining any Hit Points at
// 0 ends Unconscious (the creature stays Prone) and resets Death Saving Throws.
func (s *Session) heal(t *creature, amount int) []Event {
	amount = max(min(amount, t.MaxHP-t.hp), 0)
	if amount == 0 {
		return nil
	}
	before := t.hp
	t.hp += amount
	out := []Event{HealingReceived{Creature: t.ID, Amount: amount, HPAfter: t.hp}}
	if before == 0 {
		t.deathSucc, t.deathFail, t.stable = 0, 0, false
		out = append(out, s.clear(t, CondUnconscious)...)
	}
	return out
}

// stabilize makes a creature at 0 Hit Points Stable; the tallies reset.
func (s *Session) stabilize(t *creature) []Event {
	t.stable, t.deathSucc, t.deathFail = true, 0, 0
	return []Event{CreatureStabilized{Creature: t.ID}}
}

// deathSave rolls the Death Saving Throw a character makes when its turn
// starts at 0 Hit Points (SRD "Playing the Game", Death Saving Throws).
func (s *Session) deathSave(c *creature) []Event {
	d := s.dice.Roll(20)
	switch {
	case d == 20:
		out := []Event{DeathSaveRolled{Creature: c.ID, D20: d}}
		return append(out, s.heal(c, 1)...)
	case d == 1:
		c.deathFail += 2
	case d >= 10:
		c.deathSucc++
	default:
		c.deathFail++
	}
	out := []Event{DeathSaveRolled{Creature: c.ID, D20: d, Successes: c.deathSucc, Failures: c.deathFail}}
	switch {
	case c.deathFail >= 3:
		out = append(out, s.die(c)...)
	case c.deathSucc >= 3:
		out = append(out, s.stabilize(c)...)
	}
	return out
}

// save rolls a saving throw (SRD "Rules Glossary"): Paralyzed, Petrified,
// Stunned and Unconscious creatures automatically fail Strength and Dexterity
// saves; Restrained gives Disadvantage on Dexterity saves.
func (s *Session) save(t *creature, e SavingThrow) SaveRolled {
	r := SaveRolled{Creature: t.ID, Ability: e.Ability, DC: e.DC, Modifier: t.SaveMods[e.Ability] - t.d20Penalty()}
	if e.Ability == Strength || e.Ability == Dexterity {
		for _, k := range []Condition{CondParalyzed, CondPetrified, CondStunned, CondUnconscious} {
			if t.has(k) {
				r.AutoFail = true
				return r
			}
		}
	}
	dis := e.Ability == Dexterity && t.has(CondRestrained)
	r.D20, r.Mode = s.rollD20(false, dis)
	r.Total = r.D20 + r.Modifier
	r.Success = r.Total >= e.DC
	return r
}

// check rolls an ability check.
func (s *Session) check(t *creature, a Ability, mod, dc int, sight, hearing bool) CheckRolled {
	r := CheckRolled{Creature: t.ID, Ability: a, DC: dc, Modifier: mod - t.d20Penalty()}
	if (sight && t.has(CondBlinded)) || (hearing && t.has(CondDeafened)) {
		r.AutoFail = true
		return r
	}
	r.D20, r.Mode = s.rollD20(false, t.checkDisadvantage())
	r.Total = r.D20 + r.Modifier
	r.Success = r.Total >= dc
	return r
}

func (s *Session) stand(c *creature) ([]Event, error) {
	switch {
	case !c.own(CondProne):
		return nil, reject(NoSuchOption, "%s is not Prone", c.ID)
	case c.speed() == 0:
		return nil, reject(Unreachable, "%s has Speed 0 and can't right itself", c.ID)
	}
	cost := c.speed() / 2
	if left := s.movementLeft(c); cost > left {
		return nil, reject(Unreachable, "standing costs %d ft, %d ft left", cost, left)
	}
	s.movedFt += cost
	out := []Event{StoodUp{Creature: c.ID, CostFt: cost}}
	return append(out, s.clear(c, CondProne)...), nil
}

// stabilizeAttempt is the Help action to stabilize a creature at 0 Hit Points:
// a DC 10 Wisdom (Medicine) check.
func (s *Session) stabilizeAttempt(in Stabilize) ([]Event, error) {
	c := s.active()
	if c.incapacitated() {
		return nil, reject(Incapacitated, "%s is Incapacitated", c.ID)
	}
	if s.acted {
		return nil, reject(ActionUsed, "%s already took an action this turn", c.ID)
	}
	t, err := s.target(in.Target)
	switch {
	case err != nil:
		return nil, err
	case !t.dying():
		return nil, reject(InvalidTarget, "%s is not dying", t.ID)
	case board.Distance(c.Pos, t.Pos)*board.SquareFeet > 5:
		return nil, reject(OutOfReach, "%s is out of reach to help", t.ID)
	}
	s.acted = true
	r := s.check(c, Wisdom, c.MedicineMod, 10, false, false)
	out := []Event{r}
	if r.Success {
		out = append(out, s.stabilize(t)...)
	}
	return out, nil
}
