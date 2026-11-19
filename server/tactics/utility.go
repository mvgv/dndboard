package tactics

import (
	"maps"
	"slices"

	"github.com/mvgv/dndboard/server/board"
	"github.com/mvgv/dndboard/server/session"
)

// Personality weights what a monster values. Every score is in Hit Points of
// expected damage, so the weights read as "how much more (or less) than the
// plain numbers does this monster care".
type Personality struct {
	// Ferocity multiplies the value of damage dealt and characters dropped, and
	// the urge to close in on the enemy.
	Ferocity float64
	// Caution multiplies the cost of damage it may take (Opportunity Attacks,
	// enemies that can reach it) and, once it is badly hurt, the urge to get away.
	Caution float64
	// Loyalty is how much it values staying close to Leader and hitting the
	// enemies close to Leader.
	Loyalty float64
	Leader  session.ID
}

// Personality presets. A creature with no personality is Balanced.
var (
	Balanced  = Personality{Ferocity: 1, Caution: 1}
	Cowardly  = Personality{Ferocity: 0.5, Caution: 3}
	Ferocious = Personality{Ferocity: 1.5, Caution: 0.3}
)

// Protective is Balanced plus loyalty to the leader it guards.
func Protective(leader session.ID) Personality {
	return Personality{Ferocity: 1, Caution: 1, Loyalty: 1.5, Leader: leader}
}

const (
	// dropValue is what dropping a creature to 0 Hit Points is worth, beyond the
	// damage itself, in Hit Points; it is also what being dropped costs.
	dropValue = 6.0
	// downedWeight scales the value of hitting a creature that is already down.
	downedWeight = 0.5
	// distCap bounds the distance terms, in squares.
	distCap = 8
	// approachPerSquare is the pull toward the enemy, per square, at full health.
	approachPerSquare = 0.3
	// loyaltyPerSquare is the pull toward the leader, per square beyond adjacent.
	loyaltyPerSquare = 0.5
	// leaderRadius is how close to the leader, in squares, an enemy threatens it.
	leaderRadius = 1
	epsilon      = 1e-9
)

// Utility is the deterministic tactical brain: a utility AI that scores each
// way of spending the turn by expected value, weighted by the creature's
// Personality, and picks the best. It makes no random choice, and breaks ties by
// a fixed order (attack, move, Nimble Escape, Disengage, Dodge, end turn).
//
// It plans one step at a time from the current state: each plan (for instance
// "step to this square, then attack from there") is valued as damage dealt, minus
// damage risked at the square it ends on and on the way, and its first step is
// the choice. It knows a creature only by its CreatureSpec and what the session
// shows everyone. Support actions (Help, Hide, Search, Ready, Utilize, Stabilize,
// Dash) are not modeled yet, so it never chooses them.
type Utility struct {
	specs map[session.ID]session.CreatureSpec
	pers  map[session.ID]Personality
}

// NewUtility builds the brain for a combat from the specs of every creature in
// it (their attacks and speeds are not part of the visible state) and the
// personalities of the creatures it plays; the rest are Balanced.
func NewUtility(specs []session.CreatureSpec, personalities map[session.ID]Personality) *Utility {
	u := &Utility{specs: map[session.ID]session.CreatureSpec{}, pers: personalities}
	for _, sp := range specs {
		u.specs[sp.ID] = sp
	}
	return u
}

// foe is another creature as the planner sees it.
type foe struct {
	id   session.ID
	st   session.CreatureState
	spec session.CreatureSpec
}

// plan is the working state of one Choose call.
type plan struct {
	me     foe
	p      Personality
	la     session.LegalActions
	foes   []foe // standing enemies, sorted by ID
	leader *foe
	hurt   float64 // 0 at full Hit Points, 1 at none
}

type candidate struct {
	in    session.Intent
	score float64
}

// Choose returns the best legal intent, or ok=false when it does not know the
// active creature.
func (u *Utility) Choose(st session.State, la session.LegalActions) (session.Intent, bool) {
	spec, known := u.specs[la.Creature]
	self, present := st.Creatures[la.Creature]
	if !known || !present || !la.EndTurn {
		return nil, false
	}
	p, ok := u.pers[la.Creature]
	if !ok {
		p = Balanced
	}
	pl := &plan{me: foe{la.Creature, self, spec}, p: p, la: la}
	if self.MaxHP > 0 {
		pl.hurt = min(max(1-float64(self.HP)/float64(self.MaxHP), 0), 1)
	}
	for _, id := range slices.Sorted(maps.Keys(st.Creatures)) {
		c := st.Creatures[id]
		if c.Dead || c.Gone {
			continue
		}
		f := foe{id, c, u.specs[id]}
		if id == p.Leader && id != la.Creature {
			pl.leader = &f
		}
		if c.Team != self.Team && !c.Down {
			pl.foes = append(pl.foes, f)
		}
	}
	return pl.best(st), true
}

func (pl *plan) best(st session.State) session.Intent {
	me, la := pl.me.id, pl.la
	// Standing up from Prone is cheap and fixes Disadvantage and crawling.
	if la.Stand && len(pl.foes) > 0 {
		return session.Stand{Actor: me}
	}
	cur := pl.me.st.Pos
	here := pl.position(cur, 1)

	var best candidate
	consider := func(in session.Intent, score float64) {
		if best.in == nil || score > best.score+epsilon {
			best = candidate{in, score}
		}
	}
	// Attack from where it stands.
	for _, o := range la.Attacks {
		if t, ok := pl.enemy(st, o.Target); ok {
			consider(session.Attack{Actor: me, Target: o.Target}, pl.attackValue(t)+here)
		}
	}
	// Move, then (the next step) act from there.
	for _, m := range la.Moves {
		consider(session.Move{Actor: me, To: m.To},
			pl.bestAttack(m.To)+pl.position(m.To, 1)-pl.openingCost(m.To))
	}
	// Leave the enemy's reach without provoking.
	// Nimble Escape is a free Bonus Action, so it comes before the Disengage action.
	if la.NimbleEscape && !la.Disengaged {
		if v, ok := pl.bestMove(true); ok {
			consider(session.NimbleEscape{Actor: me}, v)
		}
	}
	if la.Disengage && !la.Disengaged {
		if v, ok := pl.bestMove(false); ok {
			consider(session.Disengage{Actor: me}, v)
		}
	}
	// Dodge halves the risk of standing here; it is what a creature with nothing
	// to hit this turn does, never a substitute for an attack.
	if la.Dodge && !pl.canAttack() {
		consider(session.Dodge{Actor: me}, pl.position(cur, 0.5))
	}
	consider(session.EndTurn{Actor: me}, here)
	return best.in
}

// canAttack reports whether an attack is possible this turn, here or after a move.
func (pl *plan) canAttack() bool {
	if len(pl.la.Attacks) > 0 {
		return true
	}
	for _, m := range pl.la.Moves {
		if pl.bestAttack(m.To) > 0 {
			return true
		}
	}
	return false
}

// bestMove values the best square to walk to without provoking Opportunity
// Attacks, with the attack that could follow when the action is still unspent.
func (pl *plan) bestMove(withAttack bool) (float64, bool) {
	var best float64
	found := false
	for _, m := range pl.la.Moves {
		v := pl.position(m.To, 1)
		if withAttack {
			v += pl.bestAttack(m.To)
		}
		if !found || v > best {
			best, found = v, true
		}
	}
	return best, found
}

func (pl *plan) enemy(st session.State, id session.ID) (foe, bool) {
	for _, f := range pl.foes {
		if f.id == id {
			return f, true
		}
	}
	// A creature at 0 Hit Points is still a legal target.
	c, ok := st.Creatures[id]
	if !ok || c.Dead || c.Gone || c.Team == pl.me.st.Team {
		return foe{}, false
	}
	return foe{id, c, session.CreatureSpec{}}, true
}

// bestAttack is the value of the best attack the creature could make from
// square x, zero if it has no action left or nothing in reach.
func (pl *plan) bestAttack(x board.Pos) float64 {
	if !pl.la.Action {
		return 0
	}
	var best float64
	for _, t := range pl.foes {
		if t.st.Hidden || board.Distance(x, t.st.Pos)*board.SquareFeet > pl.me.spec.Attack.ReachFt {
			continue
		}
		best = max(best, pl.attackValue(t))
	}
	return best
}

// attackValue is hit chance × expected damage, plus the chance to drop the
// target times what dropping it is worth, weighted by Ferocity, and by Loyalty
// when the target threatens the leader.
func (pl *plan) attackValue(t foe) float64 {
	exp, drop := strike(pl.me, t.st)
	v := exp + drop*dropValue
	if t.st.Down {
		v = exp * downedWeight
	}
	if pl.leader != nil && board.Distance(t.st.Pos, pl.leader.st.Pos) <= leaderRadius {
		v *= 1 + pl.p.Loyalty
	}
	return v * pl.p.Ferocity
}

// position values ending the turn on square x: minus the danger there (scaled
// by risk, 0.5 for a Dodging creature), the pull toward the enemy while
// healthy, the push away from it once more than half the Hit Points are gone,
// and the pull toward the leader.
func (pl *plan) position(x board.Pos, risk float64) float64 {
	// A healthy creature does not shy from danger: the cost grows as it is hurt.
	weight := pl.p.Caution * 2 * pl.hurt
	v := -weight * risk * pl.danger(x)
	if near, ok := pl.nearestFoe(x); ok {
		retreat := 2 * pl.p.Caution * max(pl.hurt-0.5, 0)
		approach := approachPerSquare * pl.p.Ferocity * (1 - pl.hurt)
		v += (retreat - approach) * float64(min(near, distCap))
	}
	if pl.leader != nil {
		v -= pl.p.Loyalty * loyaltyPerSquare * float64(min(max(board.Distance(x, pl.leader.st.Pos)-1, 0), distCap))
	}
	return v
}

func (pl *plan) nearestFoe(x board.Pos) (int, bool) {
	near, ok := 0, false
	for _, f := range pl.foes {
		if d := board.Distance(x, f.st.Pos); !ok || d < near {
			near, ok = d, true
		}
	}
	return near, ok
}

// danger is the expected damage (plus the chance of being dropped, at its
// price) from every enemy that can reach square x this coming round: it can
// walk its Speed and then strike.
func (pl *plan) danger(x board.Pos) float64 {
	var sum float64
	for _, f := range pl.foes {
		if !canAct(f.st) || f.spec.Attack.ReachFt == 0 {
			continue
		}
		reach := f.spec.Attack.ReachFt
		if canMove(f.st) {
			reach += f.spec.SpeedFt
		}
		if board.Distance(x, f.st.Pos)*board.SquareFeet > reach {
			continue
		}
		exp, drop := strike(f, pl.me.st)
		sum += exp + drop*dropValue
	}
	return sum
}

// openingCost is the weighted risk of Opportunity Attacks from walking to x:
// enemies that still have their Reaction and have the creature in reach now but
// not at x. Nothing when it has Disengaged this turn.
func (pl *plan) openingCost(x board.Pos) float64 {
	if pl.la.Disengaged {
		return 0
	}
	var sum float64
	for _, f := range pl.foes {
		if !f.st.ReactionAvailable || f.spec.Attack.ReachFt == 0 {
			continue
		}
		in := func(p board.Pos) bool { return board.Distance(p, f.st.Pos)*board.SquareFeet <= f.spec.Attack.ReachFt }
		if in(pl.me.st.Pos) && !in(x) {
			exp, drop := strike(f, pl.me.st)
			sum += exp + drop*dropValue
		}
	}
	return pl.p.Caution * (1 + 2*pl.hurt) * sum
}

func has(c session.CreatureState, k session.Condition) bool {
	return slices.Contains(c.Conditions, k)
}

func canAct(c session.CreatureState) bool {
	return !has(c, session.CondIncapacitated)
}

func canMove(c session.CreatureState) bool {
	return !has(c, session.CondRestrained) && !has(c, session.CondGrappled)
}

// strike is the expected damage of one melee attack by a against d, and the
// chance it drops d to 0 Hit Points. Hit chance follows SRD "Playing the Game"
// (Attack Rolls): d20 + modifier against AC, a natural 20 always hits and a
// natural 1 always misses; Advantage and Disadvantage come from the Conditions
// and Dodging that everyone can see. A Critical Hit adds one more set of dice.
func strike(a foe, d session.CreatureState) (exp, drop float64) {
	atk := a.spec.Attack
	if atk.DiceSides == 0 {
		return 0, 0
	}
	need := d.AC - (atk.ToHit - 2*a.st.ExhaustionLevel)
	p := min(max(float64(21-need)/20, 0.05), 0.95)
	adv, dis := attackModes(a.st, d)
	crit := 0.05
	switch {
	case adv && !dis:
		p, crit = 1-(1-p)*(1-p), 1-0.95*0.95
	case dis && !adv:
		p, crit = p*p, 0.05*0.05
	}
	avg := float64(atk.DiceCount) * float64(atk.DiceSides+1) / 2
	exp = p*(avg+float64(atk.DamageBonus)) + crit*avg

	lo := max(atk.DiceCount+atk.DamageBonus, 0)
	hi := max(atk.DiceCount*atk.DiceSides+atk.DamageBonus, 0)
	kill := 0.0
	switch hp := d.HP; {
	case hp <= lo:
		kill = 1
	case hp <= hi:
		kill = float64(hi-hp+1) / float64(hi-lo+1)
	}
	return exp, p * kill
}

func attackModes(a, d session.CreatureState) (adv, dis bool) {
	for _, k := range []session.Condition{session.CondPoisoned, session.CondFrightened, session.CondBlinded, session.CondProne, session.CondRestrained} {
		dis = dis || has(a, k)
	}
	adv = has(a, session.CondInvisible)
	for _, k := range []session.Condition{session.CondBlinded, session.CondParalyzed, session.CondRestrained, session.CondStunned, session.CondUnconscious, session.CondPetrified, session.CondProne} {
		adv = adv || has(d, k)
	}
	dis = dis || d.Dodging || has(d, session.CondInvisible)
	return adv, dis
}
