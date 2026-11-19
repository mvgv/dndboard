package session_test

import (
	"slices"
	"testing"

	"github.com/mvgv/dndboard/server/board"
	"github.com/mvgv/dndboard/server/dice"
	"github.com/mvgv/dndboard/server/session"
)

// Scenario tests for conditions, damage types and death. Each cites the SRD 5.2
// entry it encodes ("Rules Glossary" for conditions, "Playing the Game" for
// "Damage and Healing").

func mk(id session.ID, team session.Team, x, y int) session.CreatureSpec {
	return session.CreatureSpec{
		ID: id, Name: string(id), Team: team, Pos: board.Pos{X: x, Y: y},
		AC: 10, MaxHP: 20, SpeedFt: 30,
		Attack: session.MeleeAttack{Name: "Club", DiceCount: 1, DiceSides: 6, DamageType: "bludgeoning", ReachFt: 5},
	}
}

// begin starts a combat in the hall. Initiative goes in the order the creatures
// are listed (rolls 20, 15, 10, ...); rest are the dice after that.
func begin(t *testing.T, rest []int, specs ...session.CreatureSpec) *session.Session {
	t.Helper()
	var d []int
	for i := range specs {
		d = append(d, 20-5*i)
	}
	s, err := session.New(session.Config{
		Board: mustBoard(t, hall), Creatures: specs, Dice: dice.Fixed(append(d, rest...)...),
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func apply(t *testing.T, s *session.Session, e session.Effect) []session.Event {
	t.Helper()
	ev, err := s.Apply(e)
	if err != nil {
		t.Fatalf("%T rejected: %v", e, err)
	}
	return ev
}

func cond(t *testing.T, s *session.Session, id session.ID, k session.Condition, src session.ID) {
	t.Helper()
	apply(t, s, session.InflictCondition{Target: id, Condition: k, Source: src})
}

func has(s *session.Session, id session.ID, k session.Condition) bool {
	return slices.Contains(s.State().Creatures[id].Conditions, k)
}

func count[E session.Event](evs []session.Event) int {
	n := 0
	for _, e := range evs {
		if _, ok := e.(E); ok {
			n++
		}
	}
	return n
}

// passTo ends the active creature's turn, and the following ones, until id's
// next turn starts (a creature at 0 Hit Points has its turn passed at once, so
// the events are what show it came up).
func passTo(t *testing.T, s *session.Session, id session.ID) []session.Event {
	t.Helper()
	var out []session.Event
	for range 10 {
		ev := submit(t, s, session.EndTurn{Actor: s.State().Active})
		out = append(out, ev...)
		for _, e := range ev {
			if ts, ok := e.(session.TurnStarted); ok && ts.Creature == id {
				return out
			}
		}
	}
	t.Fatalf("never reached %s's turn", id)
	return nil
}

// Every condition applies and shows in the visible state.
func TestEveryConditionCanBeAppliedAndCleared(t *testing.T) {
	if len(session.AllConditions) != 15 {
		t.Fatalf("2024 has 15 conditions, got %d", len(session.AllConditions))
	}
	for _, k := range session.AllConditions {
		s := begin(t, nil, mk("a", "A", 2, 2), mk("b", "B", 3, 2))
		cond(t, s, "a", k, "b")
		if !has(s, "a", k) {
			t.Errorf("%s not in state: %v", k, s.State().Creatures["a"].Conditions)
		}
		apply(t, s, session.ClearCondition{Target: "a", Condition: k})
		if k != session.CondUnconscious && has(s, "a", k) {
			t.Errorf("%s not cleared", k)
		}
	}
}

// Attack rolls: Blinded, Invisible, Paralyzed, Petrified, Poisoned, Prone,
// Restrained, Stunned, Unconscious, Frightened and Grappled give Advantage or
// Disadvantage; Advantage and Disadvantage cancel.
func TestConditionsAffectAttackRolls(t *testing.T) {
	adv, dis, normal := session.Advantage, session.Disadvantage, session.Normal
	for _, tc := range []struct {
		name       string
		onAttacker bool
		cond       session.Condition
		want       session.RollMode
	}{
		{"Blinded attacker", true, session.CondBlinded, dis},
		{"Poisoned attacker", true, session.CondPoisoned, dis},
		{"Prone attacker", true, session.CondProne, dis},
		{"Restrained attacker", true, session.CondRestrained, dis},
		{"Frightened attacker", true, session.CondFrightened, dis},
		{"Grappled attacker (grappler elsewhere)", true, session.CondGrappled, dis},
		{"Invisible attacker", true, session.CondInvisible, adv},
		{"Charmed attacker", true, session.CondCharmed, normal},
		{"Deafened attacker", true, session.CondDeafened, normal},
		{"Blinded target", false, session.CondBlinded, adv},
		{"Paralyzed target", false, session.CondParalyzed, adv},
		{"Petrified target", false, session.CondPetrified, adv},
		{"Restrained target", false, session.CondRestrained, adv},
		{"Stunned target", false, session.CondStunned, adv},
		{"Unconscious target", false, session.CondUnconscious, adv},
		{"Prone target within 5 feet", false, session.CondProne, adv},
		{"Invisible target", false, session.CondInvisible, dis},
		{"Poisoned target", false, session.CondPoisoned, normal},
		{"Frightened target", false, session.CondFrightened, normal},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := begin(t, []int{5, 15, 3, 3}, mk("a", "A", 2, 2), mk("b", "B", 3, 2), mk("c", "B", 9, 3))
			who := session.ID("b")
			if tc.onAttacker {
				who = "a"
			}
			cond(t, s, who, tc.cond, "c")
			ev := submit(t, s, session.Attack{Actor: "a", Target: "b"})
			roll, _ := find[session.AttackRolled](ev)
			wantD20 := map[session.RollMode]int{adv: 15, dis: 5, normal: 5}[tc.want]
			if roll.Mode != tc.want || roll.D20 != wantD20 {
				t.Errorf("mode %q d20 %d, want %q d20 %d", roll.Mode, roll.D20, tc.want, wantD20)
			}
		})
	}
}

func TestAdvantageAndDisadvantageCancel(t *testing.T) {
	s := begin(t, []int{5, 15}, mk("a", "A", 2, 2), mk("b", "B", 3, 2))
	cond(t, s, "b", session.CondBlinded, "")  // Advantage against it
	cond(t, s, "a", session.CondPoisoned, "") // Disadvantage on the attack
	ev := submit(t, s, session.Attack{Actor: "a", Target: "b"})
	if roll, _ := find[session.AttackRolled](ev); roll.Mode != session.Normal || roll.D20 != 5 {
		t.Errorf("want a single normal roll of 5, got %+v", roll)
	}
}

// Grappled: Disadvantage on attack rolls against any target other than the grappler.
func TestGrappledAttackerHasNoDisadvantageAgainstTheGrappler(t *testing.T) {
	s := begin(t, []int{5, 15}, mk("a", "A", 2, 2), mk("b", "B", 3, 2))
	cond(t, s, "a", session.CondGrappled, "b")
	ev := submit(t, s, session.Attack{Actor: "a", Target: "b"})
	if roll, _ := find[session.AttackRolled](ev); roll.Mode != session.Normal {
		t.Errorf("mode %q, want normal against the grappler", roll.Mode)
	}
}

// Prone: an attack roll against you has Disadvantage if the attacker is not
// within 5 feet.
func TestAttackAgainstDistantProneTargetHasDisadvantage(t *testing.T) {
	a := mk("a", "A", 2, 2)
	a.Attack.ReachFt = 10
	s := begin(t, []int{15, 5}, a, mk("b", "B", 4, 2))
	cond(t, s, "b", session.CondProne, "")
	ev := submit(t, s, session.Attack{Actor: "a", Target: "b"})
	if roll, _ := find[session.AttackRolled](ev); roll.Mode != session.Disadvantage || roll.D20 != 5 {
		t.Errorf("got %+v", roll)
	}
}

// Paralyzed, Unconscious: any attack roll that hits is a Critical Hit if the
// attacker is within 5 feet. Stunned and Petrified give only Advantage.
func TestAutomaticCriticalHits(t *testing.T) {
	for _, tc := range []struct {
		cond session.Condition
		near bool
		crit bool
	}{
		{session.CondParalyzed, true, true},
		{session.CondUnconscious, true, true},
		{session.CondParalyzed, false, false},
		{session.CondStunned, true, false},
		{session.CondPetrified, true, false},
	} {
		a := mk("a", "A", 2, 2)
		a.Attack.ReachFt = 10
		x := 3
		if !tc.near {
			x = 4
		}
		s := begin(t, []int{5, 15, 3, 4}, a, mk("b", "B", x, 2))
		cond(t, s, "b", tc.cond, "")
		ev := submit(t, s, session.Attack{Actor: "a", Target: "b"})
		roll, _ := find[session.AttackRolled](ev)
		dmg, _ := find[session.DamageDealt](ev)
		wantDice := 1
		if tc.crit {
			wantDice = 2
		}
		if roll.Critical != tc.crit || len(dmg.Rolls) != wantDice {
			t.Errorf("%s near=%v: critical=%v with %d damage dice, want %v with %d",
				tc.cond, tc.near, roll.Critical, len(dmg.Rolls), tc.crit, wantDice)
		}
	}
}

// Incapacitated (and the conditions that include it): no action, Bonus Action
// or Reaction.
func TestIncapacitatedCreatureCannotActOrReact(t *testing.T) {
	for _, k := range []session.Condition{
		session.CondIncapacitated, session.CondParalyzed, session.CondPetrified,
		session.CondStunned, session.CondUnconscious,
	} {
		a := mk("a", "A", 2, 2)
		a.NimbleEscape = true
		s := begin(t, nil, a, mk("b", "B", 3, 2))
		cond(t, s, "a", k, "")
		for _, in := range []session.Intent{
			session.Attack{Actor: "a", Target: "b"}, session.Dash{Actor: "a"},
			session.Disengage{Actor: "a"}, session.NimbleEscape{Actor: "a"},
		} {
			if rej := rejection(t, s, in); rej.Code != session.Incapacitated {
				t.Errorf("%s %T: code %v", k, in, rej.Code)
			}
		}
		la := s.LegalActions()
		if la.Action || la.BonusAction || la.Reaction || len(la.Attacks) > 0 || la.Dash || la.Disengage {
			t.Errorf("%s: legal actions should be empty: %+v", k, la)
		}
	}
}

func TestIncapacitatedCreatureMakesNoOpportunityAttack(t *testing.T) {
	// b would normally get an Opportunity Attack when a leaves its reach.
	s := begin(t, nil, mk("a", "A", 2, 2), mk("b", "B", 3, 2))
	cond(t, s, "b", session.CondStunned, "")
	ev := submit(t, s, session.Move{Actor: "a", To: board.Pos{X: 1, Y: 2}})
	if count[session.OpportunityAttackMade](ev) != 0 {
		t.Error("an Incapacitated creature can't take a Reaction")
	}
	if s.State().Creatures["b"].ReactionAvailable {
		t.Error("ReactionAvailable should be false while Incapacitated")
	}
}

// Incapacitated: Disadvantage on Initiative if Incapacitated at the roll;
// Invisible gives Advantage; Poisoned gives Disadvantage on ability checks.
func TestInitiativeAdvantageAndDisadvantage(t *testing.T) {
	for _, tc := range []struct {
		name  string
		conds []session.Condition
		rolls []int // the d20s a rolls, then b's 10
		want  int   // the d20 kept for a
	}{
		{"Invisible", []session.Condition{session.CondInvisible}, []int{3, 18}, 18},
		{"Incapacitated", []session.Condition{session.CondIncapacitated}, []int{18, 4}, 4},
		{"Poisoned", []session.Condition{session.CondPoisoned}, []int{18, 4}, 4},
		{"Invisible and Incapacitated cancel", []session.Condition{session.CondInvisible, session.CondIncapacitated}, []int{9}, 9},
	} {
		a := mk("a", "A", 2, 2)
		a.Conditions = tc.conds
		s, err := session.New(session.Config{
			Board: mustBoard(t, hall), Creatures: []session.CreatureSpec{a, mk("b", "B", 3, 2)},
			Dice: dice.Fixed(append(tc.rolls, 10)...),
		})
		if err != nil {
			t.Fatal(err)
		}
		got := -1
		for _, e := range s.Log() {
			if r, ok := e.(session.InitiativeRolled); ok && r.Creature == "a" {
				got = r.D20
			}
		}
		if got != tc.want {
			t.Errorf("%s: kept %d, want %d", tc.name, got, tc.want)
		}
	}
}

// Speed 0: Grappled, Paralyzed, Petrified, Restrained, Unconscious. Dash can't
// raise it. Stunned, Blinded etc. leave Speed alone.
func TestConditionsThatSetSpeedToZero(t *testing.T) {
	for _, k := range session.AllConditions {
		zero := slices.Contains([]session.Condition{
			session.CondGrappled, session.CondParalyzed, session.CondPetrified,
			session.CondRestrained, session.CondUnconscious,
		}, k)
		if k == session.CondExhaustion {
			continue
		}
		s := begin(t, nil, mk("a", "A", 2, 2), mk("b", "B", 9, 2))
		cond(t, s, "a", k, "b")
		la := s.LegalActions()
		if zero && (la.MovementLeftFt != 0 || len(la.Moves) != 0) {
			t.Errorf("%s: want Speed 0, got %d ft and %d moves", k, la.MovementLeftFt, len(la.Moves))
		}
		if !zero && la.MovementLeftFt != 30 {
			t.Errorf("%s: want 30 ft, got %d", k, la.MovementLeftFt)
		}
		if zero {
			rejection(t, s, session.Move{Actor: "a", To: board.Pos{X: 3, Y: 2}})
		}
	}
}

func TestDashDoesNotRaiseSpeedZero(t *testing.T) {
	s := begin(t, nil, mk("a", "A", 2, 2), mk("b", "B", 9, 2))
	cond(t, s, "a", session.CondRestrained, "")
	ev := submit(t, s, session.Dash{Actor: "a"})
	if d, _ := find[session.Dashed](ev); d.ExtraFt != 0 || s.LegalActions().MovementLeftFt != 0 {
		t.Errorf("Dash gave %d ft", d.ExtraFt)
	}
}

// Grappling: the condition ends if the grappler has the Incapacitated condition.
func TestGrappleEndsWhenGrapplerIsIncapacitated(t *testing.T) {
	s := begin(t, nil, mk("a", "A", 2, 2), mk("b", "B", 3, 2))
	cond(t, s, "a", session.CondGrappled, "b")
	ev := apply(t, s, session.InflictCondition{Target: "b", Condition: session.CondStunned})
	if has(s, "a", session.CondGrappled) || count[session.ConditionRemoved](ev) != 1 {
		t.Error("the grapple should have ended")
	}
	if s.LegalActions().MovementLeftFt != 30 {
		t.Error("Speed should be back")
	}
}

func TestGrappledAndCharmedNeedASourceCreature(t *testing.T) {
	s := begin(t, nil, mk("a", "A", 2, 2), mk("b", "B", 3, 2))
	for _, k := range []session.Condition{session.CondGrappled, session.CondCharmed} {
		if _, err := s.Apply(session.InflictCondition{Target: "a", Condition: k}); err == nil {
			t.Errorf("%s without a source should be rejected", k)
		}
	}
}

// Exhaustion: cumulative; D20 Tests reduced by 2 per level; Speed reduced by 5
// feet per level; dead at level 6; a level is removed one at a time.
func TestExhaustion(t *testing.T) {
	a := mk("a", "A", 2, 2)
	a.Attack.ToHit = 5
	s := begin(t, []int{12, 3}, a, mk("b", "B", 3, 2))
	cond(t, s, "a", session.CondExhaustion, "")
	cond(t, s, "a", session.CondExhaustion, "")
	if st := s.State().Creatures["a"]; st.ExhaustionLevel != 2 || !has(s, "a", session.CondExhaustion) {
		t.Fatalf("level %d", st.ExhaustionLevel)
	}
	if got := s.LegalActions().MovementLeftFt; got != 20 {
		t.Errorf("Speed %d ft, want 20", got)
	}
	ev := submit(t, s, session.Attack{Actor: "a", Target: "b"})
	if roll, _ := find[session.AttackRolled](ev); roll.Modifier != 1 || roll.Total != 13 {
		t.Errorf("modifier %d total %d, want 1 and 13", roll.Modifier, roll.Total)
	}
	apply(t, s, session.ClearCondition{Target: "a", Condition: session.CondExhaustion})
	if s.State().Creatures["a"].ExhaustionLevel != 1 {
		t.Error("Long Rest style removal should take one level")
	}
}

func TestExhaustionLevelSixKills(t *testing.T) {
	s := begin(t, nil, mk("a", "A", 2, 2), mk("b", "B", 3, 2), mk("c", "A", 8, 2))
	var ev []session.Event
	for range 6 {
		ev = apply(t, s, session.InflictCondition{Target: "c", Condition: session.CondExhaustion})
	}
	if count[session.CreatureDied](ev) != 1 || !s.State().Creatures["c"].Dead {
		t.Error("level 6 Exhaustion should kill")
	}
}

// Saving throws: Paralyzed, Petrified, Stunned and Unconscious creatures
// automatically fail Strength and Dexterity saves; Restrained gives Disadvantage
// on Dexterity saves; Exhaustion reduces D20 Tests.
func TestSavingThrowsAffectedByConditions(t *testing.T) {
	for _, tc := range []struct {
		cond    session.Condition
		ability session.Ability
		auto    bool
		mode    session.RollMode
	}{
		{session.CondParalyzed, session.Strength, true, ""},
		{session.CondPetrified, session.Dexterity, true, ""},
		{session.CondStunned, session.Strength, true, ""},
		{session.CondUnconscious, session.Dexterity, true, ""},
		{session.CondParalyzed, session.Constitution, false, session.Normal},
		{session.CondStunned, session.Wisdom, false, session.Normal},
		{session.CondRestrained, session.Dexterity, false, session.Disadvantage},
		{session.CondRestrained, session.Strength, false, session.Normal},
	} {
		s := begin(t, []int{17, 6}, mk("a", "A", 2, 2), mk("b", "B", 3, 2))
		cond(t, s, "a", tc.cond, "")
		ev := apply(t, s, session.SavingThrow{Target: "a", Ability: tc.ability, DC: 10})
		r, _ := find[session.SaveRolled](ev)
		if r.AutoFail != tc.auto || r.Mode != tc.mode || (tc.auto && r.Success) {
			t.Errorf("%s %s: %+v", tc.cond, tc.ability, r)
		}
		if tc.mode == session.Disadvantage && (r.D20 != 6 || r.Success) {
			t.Errorf("Restrained Dex save should keep the lower die: %+v", r)
		}
	}
}

func TestExhaustionReducesSavesAndChecks(t *testing.T) {
	a := mk("a", "A", 2, 2)
	a.SaveMods = map[session.Ability]int{session.Wisdom: 3}
	a.CheckMods = map[session.Ability]int{session.Wisdom: 3}
	s := begin(t, []int{9, 9}, a, mk("b", "B", 3, 2))
	cond(t, s, "a", session.CondExhaustion, "")
	save, _ := find[session.SaveRolled](apply(t, s, session.SavingThrow{Target: "a", Ability: session.Wisdom, DC: 10}))
	check, _ := find[session.CheckRolled](apply(t, s, session.AbilityCheck{Target: "a", Ability: session.Wisdom, DC: 10}))
	if save.Total != 10 || !save.Success || check.Total != 10 || !check.Success {
		t.Errorf("9 + 3 - 2 should be 10: %+v %+v", save, check)
	}
}

// Blinded: automatically fail any ability check that requires sight. Deafened:
// likewise for hearing. Poisoned and Frightened: Disadvantage on ability checks.
func TestConditionsAffectAbilityChecks(t *testing.T) {
	for _, tc := range []struct {
		name        string
		cond        session.Condition
		sight, hear bool
		auto        bool
		mode        session.RollMode
	}{
		{"Blinded, sight needed", session.CondBlinded, true, false, true, ""},
		{"Blinded, no sight needed", session.CondBlinded, false, true, false, session.Normal},
		{"Deafened, hearing needed", session.CondDeafened, false, true, true, ""},
		{"Deafened, no hearing needed", session.CondDeafened, true, false, false, session.Normal},
		{"Poisoned", session.CondPoisoned, false, false, false, session.Disadvantage},
		{"Frightened", session.CondFrightened, false, false, false, session.Disadvantage},
	} {
		s := begin(t, []int{18, 4}, mk("a", "A", 2, 2), mk("b", "B", 3, 2))
		cond(t, s, "a", tc.cond, "b")
		r, _ := find[session.CheckRolled](apply(t, s, session.AbilityCheck{
			Target: "a", Ability: session.Wisdom, DC: 10, NeedsSight: tc.sight, NeedsHearing: tc.hear}))
		if r.AutoFail != tc.auto || r.Mode != tc.mode || (tc.auto && r.Success) {
			t.Errorf("%s: %+v", tc.name, r)
		}
		if tc.mode == session.Disadvantage && r.D20 != 4 {
			t.Errorf("%s: kept %d, want the lower die 4", tc.name, r.D20)
		}
	}
}

// Prone: crawling costs 1 extra foot per foot; right yourself by spending half
// your Speed; can't right yourself at Speed 0.
func TestProneCrawlsAndStands(t *testing.T) {
	s := begin(t, nil, mk("a", "A", 2, 2), mk("b", "B", 9, 2))
	cond(t, s, "a", session.CondProne, "")
	la := s.LegalActions()
	for _, m := range la.Moves {
		if m.CostFt != 10 && m.To == (board.Pos{X: 3, Y: 2}) {
			t.Errorf("crawling one square should cost 10 ft, got %d", m.CostFt)
		}
	}
	far := board.Distance(board.Pos{X: 2, Y: 2}, board.Pos{X: 6, Y: 2})
	if far != 4 {
		t.Fatal("test setup")
	}
	for _, m := range la.Moves {
		if m.To == (board.Pos{X: 6, Y: 2}) {
			t.Error("30 ft of crawling covers only 3 squares")
		}
	}
	if !la.Stand {
		t.Fatal("Stand should be legal")
	}
	ev := submit(t, s, session.Stand{Actor: "a"})
	if st, _ := find[session.StoodUp](ev); st.CostFt != 15 {
		t.Errorf("standing costs %d ft, want 15", st.CostFt)
	}
	if has(s, "a", session.CondProne) || s.LegalActions().MovementLeftFt != 15 {
		t.Error("should be on its feet with 15 ft left")
	}
	if rej := rejection(t, s, session.Stand{Actor: "a"}); rej.Code != session.NoSuchOption {
		t.Errorf("not prone: %v", rej.Code)
	}
}

func TestProneRestrainedCreatureCannotStand(t *testing.T) {
	s := begin(t, nil, mk("a", "A", 2, 2), mk("b", "B", 9, 2))
	cond(t, s, "a", session.CondProne, "")
	cond(t, s, "a", session.CondRestrained, "")
	if s.LegalActions().Stand {
		t.Error("Speed 0 can't right itself")
	}
	rejection(t, s, session.Stand{Actor: "a"})
}

// Frightened: can't willingly move closer to the source of fear.
func TestFrightenedCannotApproachTheSource(t *testing.T) {
	s := begin(t, nil, mk("a", "A", 3, 2), mk("b", "B", 7, 2))
	cond(t, s, "a", session.CondFrightened, "b")
	start := board.Distance(board.Pos{X: 3, Y: 2}, board.Pos{X: 7, Y: 2})
	moves := s.LegalActions().Moves
	if len(moves) == 0 {
		t.Fatal("it can still move away")
	}
	for _, m := range moves {
		if board.Distance(m.To, board.Pos{X: 7, Y: 2}) < start {
			t.Errorf("%v is closer to the source", m.To)
		}
	}
}

// Charmed: can't attack the charmer.
func TestCharmedCreatureCannotAttackTheCharmer(t *testing.T) {
	s := begin(t, []int{5, 5}, mk("a", "A", 2, 2), mk("b", "B", 3, 2), mk("c", "B", 2, 3))
	cond(t, s, "a", session.CondCharmed, "b")
	if rej := rejection(t, s, session.Attack{Actor: "a", Target: "b"}); rej.Code != session.InvalidTarget {
		t.Errorf("code %v", rej.Code)
	}
	for _, o := range s.LegalActions().Attacks {
		if o.Target == "b" {
			t.Error("the charmer is not a legal target")
		}
	}
	submit(t, s, session.Attack{Actor: "a", Target: "c"})
}

// Unconscious: includes Incapacitated and Prone; Petrified includes Incapacitated.
// When Unconscious ends the creature remains Prone.
func TestImpliedConditions(t *testing.T) {
	s := begin(t, nil, mk("a", "A", 2, 2), mk("b", "B", 3, 2))
	cond(t, s, "a", session.CondUnconscious, "")
	for _, k := range []session.Condition{session.CondIncapacitated, session.CondProne} {
		if !has(s, "a", k) {
			t.Errorf("Unconscious should include %s", k)
		}
	}
	apply(t, s, session.ClearCondition{Target: "a", Condition: session.CondUnconscious})
	if has(s, "a", session.CondUnconscious) || !has(s, "a", session.CondProne) {
		t.Errorf("should remain Prone: %v", s.State().Creatures["a"].Conditions)
	}
}

// Petrified: Resistance to all damage and Immunity to the Poisoned condition.
func TestPetrified(t *testing.T) {
	s := begin(t, nil, mk("a", "A", 2, 2), mk("b", "B", 3, 2))
	cond(t, s, "a", session.CondPoisoned, "")
	cond(t, s, "a", session.CondPetrified, "")
	if has(s, "a", session.CondPoisoned) {
		t.Error("Petrified is immune to Poisoned")
	}
	if ev := apply(t, s, session.InflictCondition{Target: "a", Condition: session.CondPoisoned}); count[session.ConditionResisted](ev) != 1 {
		t.Error("Poisoned should be resisted")
	}
	ev := apply(t, s, session.InflictDamage{Target: "a", Amount: 9, Type: "fire"})
	if d, _ := find[session.DamageDealt](ev); d.Amount != 4 || d.Adjustment != "resistance" {
		t.Errorf("%+v", d)
	}
}

func TestConditionImmunityFromTheStatBlock(t *testing.T) {
	a := mk("a", "A", 2, 2)
	a.ConditionImmunities = []session.Condition{session.CondCharmed}
	s := begin(t, nil, a, mk("b", "B", 3, 2))
	ev := apply(t, s, session.InflictCondition{Target: "a", Condition: session.CondCharmed, Source: "b"})
	if count[session.ConditionResisted](ev) != 1 || has(s, "a", session.CondCharmed) {
		t.Error("should be immune to Charmed")
	}
}

// Resistance halves damage (round down), Vulnerability doubles it, Immunity
// negates it; duplicates count once; Resistance applies before Vulnerability.
func TestDamageResistanceVulnerabilityImmunity(t *testing.T) {
	for _, tc := range []struct {
		name           string
		res, vul, imm  []string
		amount         int
		dtype          string
		want           int
		wantAdjustment string
	}{
		{"resistance rounds down", []string{"fire"}, nil, nil, 7, "fire", 3, "resistance"},
		{"vulnerability doubles", nil, []string{"cold"}, nil, 7, "cold", 14, "vulnerability"},
		{"immunity", nil, nil, []string{"poison"}, 7, "poison", 0, "immunity"},
		{"other types are unaffected", []string{"fire"}, []string{"cold"}, []string{"poison"}, 7, "slashing", 7, ""},
		{"duplicate resistance counts once", []string{"fire", "Fire"}, nil, nil, 8, "fire", 4, "resistance"},
		{"resistance then vulnerability", []string{"fire"}, []string{"fire"}, nil, 11, "fire", 10, "resistance+vulnerability"},
		{"immunity beats the rest", []string{"fire"}, []string{"fire"}, []string{"fire"}, 11, "fire", 0, "immunity"},
	} {
		a := mk("a", "A", 2, 2)
		a.Resistances, a.Vulnerabilities, a.Immunities = tc.res, tc.vul, tc.imm
		s := begin(t, nil, a, mk("b", "B", 3, 2))
		ev := apply(t, s, session.InflictDamage{Target: "a", Amount: tc.amount, Type: tc.dtype})
		d, _ := find[session.DamageDealt](ev)
		if d.Amount != tc.want || d.Raw != tc.amount || d.Adjustment != tc.wantAdjustment || d.HPAfter != 20-tc.want {
			t.Errorf("%s: %+v", tc.name, d)
		}
	}
}

func TestResistanceAppliesToAttackDamageAfterTheCriticalHitDice(t *testing.T) {
	b := mk("b", "B", 3, 2)
	b.Resistances = []string{"bludgeoning"}
	s := begin(t, []int{20, 3, 4}, mk("a", "A", 2, 2), b)
	ev := submit(t, s, session.Attack{Actor: "a", Target: "b"})
	d, _ := find[session.DamageDealt](ev)
	if len(d.Rolls) != 2 || d.Raw != 7 || d.Amount != 3 {
		t.Errorf("2d6 = 7 halved to 3: %+v", d)
	}
}

// Dropping to 0 Hit Points: a monster dies the instant it drops to 0.
func TestMonsterDiesAtZeroHitPoints(t *testing.T) {
	s := begin(t, nil, mk("a", "A", 2, 2), mk("b", "B", 3, 2), mk("c", "B", 9, 2))
	ev := apply(t, s, session.InflictDamage{Target: "b", Amount: 20, Type: "fire"})
	if count[session.CreatureDowned](ev) != 1 || count[session.CreatureDied](ev) != 1 || !s.State().Creatures["b"].Dead {
		t.Errorf("%v", ev)
	}
}

// party: a Player Character at 1,2 with a friend at 1,3 and a monster at 2,2.
func party(t *testing.T, rest ...int) *session.Session {
	t.Helper()
	p1 := mk("p1", "P", 1, 2)
	p1.Character = true
	p2 := mk("p2", "P", 1, 3)
	p2.Character = true
	return begin(t, rest, p1, mk("m", "M", 2, 2), p2)
}

// Falling Unconscious: a character that reaches 0 Hit Points and doesn't die
// instantly has the Unconscious condition.
func TestCharacterFallsUnconsciousAtZeroHitPoints(t *testing.T) {
	s := party(t)
	ev := apply(t, s, session.InflictDamage{Target: "p1", Amount: 25, Type: "fire"})
	st := s.State().Creatures["p1"]
	if !st.Down || st.Dead || st.HP != 0 || !has(s, "p1", session.CondUnconscious) || count[session.CreatureDied](ev) != 0 {
		t.Errorf("%+v", st)
	}
	if s.State().Over {
		t.Error("the party still has p2")
	}
}

// Massive Damage: dies if the damage remaining after dropping to 0 equals or
// exceeds the Hit Point maximum (maximum 20).
func TestMassiveDamageKillsACharacter(t *testing.T) {
	s := party(t)
	apply(t, s, session.InflictDamage{Target: "p1", Amount: 6, Type: "fire"})
	// 14 HP left; 33 damage leaves 19 over, below the maximum of 20.
	apply(t, s, session.InflictDamage{Target: "p1", Amount: 33, Type: "fire"})
	if s.State().Creatures["p1"].Dead {
		t.Fatal("19 remaining is below the maximum")
	}
	s = party(t)
	apply(t, s, session.InflictDamage{Target: "p1", Amount: 6, Type: "fire"})
	ev := apply(t, s, session.InflictDamage{Target: "p1", Amount: 34, Type: "fire"})
	if !s.State().Creatures["p1"].Dead || count[session.CreatureDied](ev) != 1 {
		t.Error("20 remaining equals the maximum: dead")
	}
}

// Death Saving Throws: rolled when the turn starts at 0 Hit Points; 10 or
// higher succeeds, otherwise fails; a 1 is two failures.
func TestDeathSavingThrowsAtTurnStart(t *testing.T) {
	s := party(t, 10, 9, 1)
	apply(t, s, session.InflictDamage{Target: "p1", Amount: 25, Type: "fire"})
	for i, want := range [][2]int{{1, 0}, {1, 1}} {
		ev := passTo(t, s, "p1")
		r, ok := find[session.DeathSaveRolled](ev)
		if !ok || r.Successes != want[0] || r.Failures != want[1] {
			t.Errorf("roll %d: %+v", i, r)
		}
		if s.State().Active == "p1" {
			t.Error("a creature at 0 Hit Points does nothing on its turn")
		}
	}
	// A natural 1 is two failures: 1 + 2 = 3, dead.
	ev := passTo(t, s, "p1")
	if r, _ := find[session.DeathSaveRolled](ev); r.D20 != 1 || count[session.CreatureDied](ev) != 1 {
		t.Errorf("a natural 1 should be two failures and kill: %+v", ev)
	}
	st := s.State().Creatures["p1"]
	if !st.Dead || s.State().Over {
		t.Errorf("p1 dead, combat goes on with p2: %+v over=%v", st, s.State().Over)
	}
}

func TestDeathSaveOfExactlyTenSucceedsAndNineFails(t *testing.T) {
	s := party(t, 10, 9)
	apply(t, s, session.InflictDamage{Target: "p1", Amount: 25, Type: "fire"})
	submit(t, s, session.EndTurn{Actor: "p1"})
	passTo(t, s, "p1")
	if st := s.State().Creatures["p1"]; st.DeathSaveSuccesses != 1 || st.DeathSaveFailures != 0 {
		t.Errorf("10 succeeds: %+v", st)
	}
	passTo(t, s, "p1")
	if st := s.State().Creatures["p1"]; st.DeathSaveSuccesses != 1 || st.DeathSaveFailures != 1 {
		t.Errorf("9 fails, tallies are not consecutive: %+v", st)
	}
}

// Three successes make a creature Stable; the tallies reset.
func TestThreeDeathSaveSuccessesStabilize(t *testing.T) {
	s := party(t, 12, 15, 10)
	apply(t, s, session.InflictDamage{Target: "p1", Amount: 25, Type: "fire"})
	submit(t, s, session.EndTurn{Actor: "p1"})
	var ev []session.Event
	for range 3 {
		ev = passTo(t, s, "p1")
	}
	st := s.State().Creatures["p1"]
	if count[session.CreatureStabilized](ev) != 1 || !st.Stable || st.DeathSaveSuccesses != 0 || st.Dead {
		t.Errorf("%+v", st)
	}
	if !has(s, "p1", session.CondUnconscious) {
		t.Error("a Stable creature is still Unconscious")
	}
	// Stable: no more death saves (the script has no dice left).
	passTo(t, s, "p1")
	passTo(t, s, "p1")
}

// Rolling a 20 on a Death Saving Throw regains 1 Hit Point: the creature is up
// (but Prone) and plays its turn.
func TestNatural20OnDeathSaveRegainsAHitPoint(t *testing.T) {
	s := party(t, 20)
	apply(t, s, session.InflictDamage{Target: "p1", Amount: 25, Type: "fire"})
	submit(t, s, session.EndTurn{Actor: "p1"})
	passTo(t, s, "p1")
	st := s.State().Creatures["p1"]
	if st.HP != 1 || has(s, "p1", session.CondUnconscious) || !has(s, "p1", session.CondProne) || st.Down {
		t.Errorf("%+v", st)
	}
	if s.State().Active != "p1" || !s.LegalActions().Action {
		t.Error("the revived creature takes its turn")
	}
}

// Damage at 0 Hit Points: one Death Saving Throw failure, two on a Critical Hit.
func TestDamageAtZeroHitPointsCausesFailures(t *testing.T) {
	s := party(t)
	apply(t, s, session.InflictDamage{Target: "p1", Amount: 25, Type: "fire"})
	ev := apply(t, s, session.InflictDamage{Target: "p1", Amount: 1, Type: "fire"})
	if f, _ := find[session.DeathSaveFailed](ev); f.Failures != 1 {
		t.Errorf("%+v", f)
	}
	apply(t, s, session.InflictDamage{Target: "p1", Amount: 1, Type: "fire", Critical: true})
	if !s.State().Creatures["p1"].Dead {
		t.Error("1 + 2 failures make three: dead")
	}
}

func TestAttackOnUnconsciousCharacterIsACriticalHitWithTwoFailures(t *testing.T) {
	s := party(t, 5, 15, 2, 2)
	apply(t, s, session.InflictDamage{Target: "p1", Amount: 25, Type: "fire"})
	submit(t, s, session.EndTurn{Actor: "p1"}) // m's turn; p1 is within 5 feet
	ev := submit(t, s, session.Attack{Actor: "m", Target: "p1"})
	if r, _ := find[session.AttackRolled](ev); !r.Critical || r.Mode != session.Advantage {
		t.Errorf("%+v", r)
	}
	if f, _ := find[session.DeathSaveFailed](ev); f.Failures != 2 {
		t.Errorf("a Critical Hit is two failures: %+v", f)
	}
}

func TestDamageEqualToMaximumAtZeroHitPointsKills(t *testing.T) {
	s := party(t)
	apply(t, s, session.InflictDamage{Target: "p1", Amount: 25, Type: "fire"})
	apply(t, s, session.InflictDamage{Target: "p1", Amount: 20, Type: "fire"})
	if !s.State().Creatures["p1"].Dead {
		t.Error("damage equal to the Hit Point maximum kills")
	}
}

func TestImmuneDamageAtZeroHitPointsIsNotDamage(t *testing.T) {
	p1 := mk("p1", "P", 1, 2)
	p1.Character, p1.Immunities = true, []string{"poison"}
	s := begin(t, nil, p1, mk("m", "M", 2, 2), mk("p2", "P", 1, 3))
	apply(t, s, session.InflictDamage{Target: "p1", Amount: 25, Type: "fire"})
	ev := apply(t, s, session.InflictDamage{Target: "p1", Amount: 5, Type: "poison"})
	if count[session.DeathSaveFailed](ev) != 0 {
		t.Error("0 damage taken is no failure")
	}
}

// Stabilizing: the Help action with a DC 10 Wisdom (Medicine) check.
func TestStabilizeWithAMedicineCheck(t *testing.T) {
	for _, tc := range []struct {
		roll   int
		stable bool
	}{{10, true}, {9, false}} {
		s := party(t, tc.roll)
		apply(t, s, session.InflictDamage{Target: "p1", Amount: 25, Type: "fire"})
		apply(t, s, session.InflictDamage{Target: "p1", Amount: 1, Type: "fire"}) // one failure
		passTo(t, s, "p2")
		if la := s.LegalActions(); !slices.Equal(la.Stabilize, []session.ID{"p1"}) {
			t.Fatalf("stabilize options %v", la.Stabilize)
		}
		ev := submit(t, s, session.Stabilize{Actor: "p2", Target: "p1"})
		st := s.State().Creatures["p1"]
		if c, _ := find[session.CheckRolled](ev); c.Ability != session.Wisdom || c.DC != 10 || c.Success != tc.stable {
			t.Errorf("%+v", c)
		}
		if st.Stable != tc.stable || (tc.stable && st.DeathSaveFailures != 0) {
			t.Errorf("roll %d: %+v", tc.roll, st)
		}
		if rej := rejection(t, s, session.Stabilize{Actor: "p2", Target: "p1"}); rej.Code != session.ActionUsed {
			t.Errorf("the Help action is spent: %v", rej.Code)
		}
	}
}

func TestStabilizeNeedsADyingCreatureWithinReach(t *testing.T) {
	s := party(t)
	passTo(t, s, "p2")
	rejection(t, s, session.Stabilize{Actor: "p2", Target: "p1"}) // not at 0 HP
	apply(t, s, session.InflictDamage{Target: "p1", Amount: 25, Type: "fire"})
	s2 := party(t)
	apply(t, s2, session.InflictDamage{Target: "p2", Amount: 25, Type: "fire"})
	// p1 and p2 are adjacent, so move p1 away: out of reach.
	submit(t, s2, session.Move{Actor: "p1", To: board.Pos{X: 1, Y: 1}})
	if rej := rejection(t, s2, session.Stabilize{Actor: "p1", Target: "p2"}); rej.Code != session.OutOfReach {
		t.Errorf("code %v", rej.Code)
	}
}

// A Stable creature that takes damage stops being Stable and starts making
// Death Saving Throws again.
func TestDamageEndsStability(t *testing.T) {
	s := party(t, 12)
	apply(t, s, session.InflictDamage{Target: "p1", Amount: 25, Type: "fire"})
	passTo(t, s, "p2")
	submit(t, s, session.Stabilize{Actor: "p2", Target: "p1"})
	ev := apply(t, s, session.InflictDamage{Target: "p1", Amount: 2, Type: "fire"})
	st := s.State().Creatures["p1"]
	if st.Stable || st.DeathSaveFailures != 1 || count[session.DeathSaveFailed](ev) != 1 {
		t.Errorf("%+v", st)
	}
}

// Healing at 0 Hit Points ends Unconscious (the creature stays Prone) and
// resets Death Saving Throws.
func TestHealingAtZeroHitPoints(t *testing.T) {
	s := party(t)
	apply(t, s, session.InflictDamage{Target: "p1", Amount: 25, Type: "fire"})
	apply(t, s, session.InflictDamage{Target: "p1", Amount: 1, Type: "fire"})
	apply(t, s, session.Heal{Target: "p1", Amount: 4})
	st := s.State().Creatures["p1"]
	if st.HP != 4 || st.DeathSaveFailures != 0 || has(s, "p1", session.CondUnconscious) || !has(s, "p1", session.CondProne) {
		t.Errorf("%+v", st)
	}
	apply(t, s, session.Heal{Target: "p1", Amount: 100})
	if s.State().Creatures["p1"].HP != 20 {
		t.Error("healing is capped at the maximum")
	}
}

func TestTheDeadCannotBeHealedOrAttacked(t *testing.T) {
	s := party(t)
	apply(t, s, session.InflictDamage{Target: "p1", Amount: 45, Type: "fire"})
	if _, err := s.Apply(session.Heal{Target: "p1", Amount: 5}); err == nil {
		t.Error("the dead can't be healed")
	}
	passTo(t, s, "m")
	if rej := rejection(t, s, session.Attack{Actor: "m", Target: "p1"}); rej.Code != session.InvalidTarget {
		t.Errorf("code %v", rej.Code)
	}
}

// The combat ends when the whole party is down (nobody left to act).
func TestCombatEndsWhenEveryCharacterIsDown(t *testing.T) {
	s := party(t)
	apply(t, s, session.InflictDamage{Target: "p1", Amount: 25, Type: "fire"})
	ev := apply(t, s, session.InflictDamage{Target: "p2", Amount: 25, Type: "fire"})
	if e, ok := find[session.CombatEnded](ev); !ok || e.Winner != "M" || !s.State().Over {
		t.Errorf("%v", ev)
	}
	if count[session.CombatEnded](s.Log()) != 1 {
		t.Error("CombatEnded must be logged once")
	}
}
