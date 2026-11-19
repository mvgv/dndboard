// Package table seats the Players' clients around one combat: it accepts their
// intents for the Player Characters, lets a Brain play everyone else, and
// broadcasts the resulting events, visible state and legal actions to every
// client. The session behind it stays the sole mechanical authority (ADR 0001);
// the table only routes and projects.
package table

import (
	"fmt"
	"slices"
	"sync"

	"github.com/mvgv/dndboard/server/board"
	"github.com/mvgv/dndboard/server/dice"
	"github.com/mvgv/dndboard/server/session"
)

// Brain chooses an intent for the active creature when it is not a Player
// Character. A rejected choice ends that creature's turn.
type Brain interface {
	Choose(session.State, session.LegalActions) session.Intent
}

// Config starts a table on a hand-built Board.
type Config struct {
	// Rows is the Board as text (see board.Parse).
	Rows      []string
	Creatures []session.CreatureSpec
	// PlayerCharacters lists the creatures the Players control; every other
	// creature is played by Brain.
	PlayerCharacters []session.ID
	Seed             uint64
	Dice             dice.Roller
	Brain            Brain
}

// buffer is how many messages a client may fall behind before it is dropped.
const buffer = 64

// maxBrainSteps bounds the intents the Brain may submit between two Player
// turns, so a misbehaving Brain cannot hang the table.
const maxBrainSteps = 1000

// Table is one combat shared by every connected client. It is safe for
// concurrent use.
type Table struct {
	mu    sync.Mutex
	s     *session.Session
	board Board
	pcs   map[session.ID]bool
	// dieSides is each creature's attack damage die, for the client's dice.
	dieSides map[session.ID]int
	brain    Brain
	clients  map[chan Message]struct{}
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
		clients:  map[chan Message]struct{}{},
	}
	for _, c := range cfg.Creatures {
		t.dieSides[c.ID] = c.Attack.DiceSides
	}
	for _, id := range cfg.PlayerCharacters {
		if _, ok := s.State().Creatures[id]; !ok {
			return nil, fmt.Errorf("table: Player Character %q is not in the combat", id)
		}
		t.pcs[id] = true
	}
	t.playBrain()
	return t, nil
}

func (t *Table) pc(id session.ID) bool { return t.pcs[id] }

func (t *Table) sides(id session.ID) int { return t.dieSides[id] }

// Join seats a client. It returns the welcome message, the channel of later
// updates, and leave, which unseats the client and closes the channel. A
// client that falls too far behind is unseated and its channel closed.
func (t *Table) Join() (welcome Message, updates <-chan Message, leave func()) {
	t.mu.Lock()
	defer t.mu.Unlock()
	ch := make(chan Message, buffer)
	t.clients[ch] = struct{}{}
	board := t.board
	board.Rows = slices.Clone(board.Rows)
	welcome = t.message("welcome", t.s.Log())
	welcome.Board = &board
	return welcome, ch, func() {
		t.mu.Lock()
		defer t.mu.Unlock()
		t.drop(ch)
	}
}

func (t *Table) drop(ch chan Message) {
	if _, ok := t.clients[ch]; ok {
		delete(t.clients, ch)
		close(ch)
	}
}

// Submit resolves a Player's intent for a Player Character, plays the Brain's
// creatures until the next Player Character's turn, and broadcasts one update
// to every client. An illegal intent returns a *session.Rejection and
// broadcasts nothing.
func (t *Table) Submit(in Intent) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.pcs[session.ID(in.Actor)] {
		return &session.Rejection{Code: session.NotYourTurn,
			Reason: fmt.Sprintf("%s is not a Player Character", quote(in.Actor))}
	}
	si, err := in.sessionIntent()
	if err != nil {
		return err
	}
	evs, err := t.s.Submit(si)
	if err != nil {
		return err
	}
	evs = append(evs, t.playBrain()...)
	m := t.message("update", evs)
	for ch := range t.clients {
		select {
		case ch <- m:
		default:
			t.drop(ch)
		}
	}
	return nil
}

// playBrain lets the Brain act while a creature it plays is active. It stops
// on a Reaction Prompt: the game is paused until the prompt is answered.
func (t *Table) playBrain() []session.Event {
	var out []session.Event
	for range maxBrainSteps {
		st := t.s.State()
		if st.Over || st.Pending != nil || t.pcs[st.Active] {
			return out
		}
		evs, err := t.s.Submit(t.brain.Choose(st, t.s.LegalActions()))
		if err != nil {
			evs, _ = t.s.Submit(session.EndTurn{Actor: st.Active})
		}
		out = append(out, evs...)
	}
	return out
}

func (t *Table) message(typ string, evs []session.Event) Message {
	st := t.s.State()
	m := Message{Type: typ, Events: wireEvents(evs, t.pc, t.sides), State: wireState(st, t.pc)}
	if !st.Over && st.Pending == nil && t.pcs[st.Active] {
		m.Legal = wireLegal(t.s.LegalActions())
	}
	return m
}

func pos(s Square) board.Pos    { return board.Pos{X: s.X, Y: s.Y} }
func square(p board.Pos) Square { return Square{X: p.X, Y: p.Y} }
