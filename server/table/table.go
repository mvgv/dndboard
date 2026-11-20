// Package table seats the Players' clients around one combat. Each client plays
// one Player Character and sees only that character's View (Glossary, View):
// the table accepts its intents for that character, lets a tactics.Brain play
// the monsters, and sends every client the events its character perceived, its
// View and, on its turn, its Legal Actions. The session behind it stays the
// sole mechanical authority (ADR 0001) and its complete State never leaves the
// server; the table only routes, filters and projects.
package table

import (
	"context"
	"fmt"
	"log"
	"slices"
	"sync"
	"time"

	"github.com/mvgv/dndboard/server/board"
	"github.com/mvgv/dndboard/server/dice"
	"github.com/mvgv/dndboard/server/session"
	"github.com/mvgv/dndboard/server/tactics"
)

// Config starts a table on a hand-built Board.
type Config struct {
	// Rows is the Board as text (see board.Parse).
	Rows []string
	// Creatures with PlayerCharacter set are played from the clients; every
	// other creature is played by Brain.
	Creatures []session.CreatureSpec
	Seed      uint64
	Dice      dice.Roller
	Brain     tactics.Brain
}

// buffer is how many messages a client may fall behind before it is dropped.
const buffer = 64

// maxBrainSteps bounds the intents the Brain may submit between two Player
// turns, so a misbehaving Brain cannot hang the table.
const maxBrainSteps = 1000

type client struct {
	viewer session.ID // the Player Character it plays and sees through
	ch     chan Message
}

// Table is one combat shared by every connected client. It is safe for
// concurrent use.
type Table struct {
	mu    sync.Mutex
	s     *session.Session
	board Board
	pcs   map[session.ID]bool
	// dieSides is each creature's attack damage die, for the client's dice.
	dieSides map[session.ID]int
	brain    tactics.Brain
	clients  map[*client]struct{}
	// seen is, per Player Character, every event it perceived so far, and batch
	// those since the last update. Events are filtered right after each
	// session call, while the state still matches them (Session.EventsFor).
	seen, batch map[session.ID][]session.Event
}

// New starts the combat and plays the Brain's creatures until a Player
// Character's turn.
func New(cfg Config) (*Table, error) {
	b, err := board.Parse(cfg.Rows)
	if err != nil {
		return nil, err
	}
	if cfg.Brain == nil {
		return nil, fmt.Errorf("table: no brain")
	}
	s, err := session.New(session.Config{Board: b, Creatures: cfg.Creatures, Seed: cfg.Seed, Dice: cfg.Dice})
	if err != nil {
		return nil, err
	}
	t := &Table{
		s:        s,
		board:    Board{Width: len(cfg.Rows[0]), Height: len(cfg.Rows), Rows: slices.Clone(cfg.Rows)},
		pcs:      map[session.ID]bool{},
		dieSides: map[session.ID]int{},
		brain:    cfg.Brain,
		clients:  map[*client]struct{}{},
		seen:     map[session.ID][]session.Event{},
		batch:    map[session.ID][]session.Event{},
	}
	for _, c := range cfg.Creatures {
		t.dieSides[c.ID] = c.Attack.DiceSides
		if c.PlayerCharacter {
			t.pcs[c.ID] = true
		}
	}
	t.record(s.Log())
	t.playBrain()
	t.flush()
	return t, nil
}

func (t *Table) pc(id session.ID) bool { return t.pcs[id] }

func (t *Table) sides(id session.ID) int { return t.dieSides[id] }

// Join seats a client playing the Player Character viewer. It returns the
// welcome message (the Board, the events viewer perceived so far, its View and
// Legal Actions), the channel of later updates, and leave, which unseats the
// client and closes the channel. A client that falls too far behind is
// unseated and its channel closed.
func (t *Table) Join(viewer string) (welcome Message, updates <-chan Message, leave func(), err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	v := session.ID(viewer)
	if !t.pcs[v] {
		return Message{}, nil, nil, fmt.Errorf("table: %s is not a Player Character", quote(viewer))
	}
	c := &client{viewer: v, ch: make(chan Message, buffer)}
	t.clients[c] = struct{}{}
	b := t.board
	b.Rows = slices.Clone(b.Rows)
	welcome = t.message("welcome", v, t.seen[v])
	welcome.Board = &b
	return welcome, c.ch, func() {
		t.mu.Lock()
		defer t.mu.Unlock()
		t.drop(c)
	}, nil
}

func (t *Table) drop(c *client) {
	if _, ok := t.clients[c]; ok {
		delete(t.clients, c)
		close(c.ch)
	}
}

// Submit resolves the intent of the client playing viewer, plays the Brain's
// creatures until the next Player Character's turn, and sends every client an
// update. A client acts only for its own Player Character. An illegal intent
// returns a *session.Rejection and sends nothing.
func (t *Table) Submit(viewer string, in Intent) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if in.Actor != viewer {
		return &session.Rejection{Code: session.NotYourTurn,
			Reason: fmt.Sprintf("this client plays %s, not %s", quote(viewer), quote(in.Actor))}
	}
	si, err := in.sessionIntent()
	if err != nil {
		return err
	}
	evs, err := t.s.Submit(si)
	if err != nil {
		return err
	}
	t.record(evs)
	t.playBrain()
	t.flush()
	return nil
}

// Advance tells the table that d of waiting time has passed. When a pending
// Reaction Prompt runs out of time, the creature does not react, play resumes
// (the Brain's creatures act until a Player Character's turn) and every client
// gets an update. Without a pending prompt it does nothing.
func (t *Table) Advance(d time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()
	evs := t.s.Advance(d)
	if len(evs) == 0 {
		return
	}
	t.record(evs)
	t.playBrain()
	t.flush()
}

// Run advances the table's clock every interval until ctx is done, so pending
// Reaction Prompts time out in real time.
func (t *Table) Run(ctx context.Context, every time.Duration) {
	tick := time.NewTicker(every)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			t.Advance(every)
		}
	}
}

// record files the events each Player Character perceives. Call it right after
// the session call that produced them.
func (t *Table) record(evs []session.Event) {
	for id := range t.pcs {
		t.batch[id] = append(t.batch[id], t.s.EventsFor(id, evs)...)
	}
}

// flush sends every client an update with its character's events since the
// last one, and moves them into that character's history.
func (t *Table) flush() {
	for c := range t.clients {
		select {
		case c.ch <- t.message("update", c.viewer, t.batch[c.viewer]):
		default:
			t.drop(c)
		}
	}
	for id, evs := range t.batch {
		t.seen[id] = append(t.seen[id], evs...)
		delete(t.batch, id)
	}
}

// playBrain lets the Brain act while a creature it plays is active, one intent
// at a time so each step's events are filtered against the state they happened
// in. A choice outside the Legal Actions ends the creature's turn. It stops on
// a Reaction Prompt: the game is paused until the prompt is answered or times
// out (Advance).
func (t *Table) playBrain() {
	for range maxBrainSteps {
		st := t.s.State()
		if st.Over || st.Pending != nil || t.pcs[st.Active] {
			return
		}
		la := t.s.LegalActions()
		in, ok := t.brain.Choose(st, la)
		if !ok || !tactics.Legal(la, in) {
			in = session.EndTurn{Actor: st.Active}
		}
		evs, err := t.s.Submit(in)
		if err != nil {
			if evs, err = t.s.Submit(session.EndTurn{Actor: st.Active}); err != nil {
				log.Printf("table: %s's turn is stuck: ending it was refused: %v", st.Active, err)
				return
			}
		}
		t.record(evs)
	}
	log.Printf("table: the Brain took %d steps without reaching a Player Character's turn", maxBrainSteps)
}

// message builds what the client playing viewer is sent: its filtered events,
// its View, and its Legal Actions while it is its turn.
func (t *Table) message(typ string, viewer session.ID, evs []session.Event) Message {
	st := t.s.State()
	m := Message{Type: typ, Events: wireEvents(evs, t.pc, t.sides),
		State: wireView(t.s.ViewFor(viewer), st.Pending, t.pc)}
	if !st.Over && st.Pending == nil && st.Active == viewer {
		m.Legal = wireLegal(t.s.LegalActions())
	}
	return m
}

func pos(s Square) board.Pos    { return board.Pos{X: s.X, Y: s.Y} }
func square(p board.Pos) Square { return Square{X: p.X, Y: p.Y} }
