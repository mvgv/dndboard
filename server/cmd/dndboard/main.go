// Command dndboard serves the lobby (login, Campaigns, invite codes, chat) and,
// once a Campaign starts, one cooperative combat on a hand-built Board to the
// Godot client over WebSocket: two Fighters, each played by the Player who
// claimed it (ws://host/play?campaign=ID&pc=fighter-1), against three Goblin
// Warriors run by the deterministic tactical brain. The -dev flag allows solo
// Campaigns.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/mvgv/dndboard/server/board"
	"github.com/mvgv/dndboard/server/lobby"
	"github.com/mvgv/dndboard/server/session"
	"github.com/mvgv/dndboard/server/srd"
	"github.com/mvgv/dndboard/server/table"
	"github.com/mvgv/dndboard/server/tactics"
	"github.com/mvgv/dndboard/server/ws"
)

// arena: '.' floor, '#' wall, '~' Difficult Terrain.
var arena = []string{
	"##############",
	"#............#",
	"#....##......#",
	"#....##...~~.#",
	"#.........~~.#",
	"#..~.........#",
	"#............#",
	"##############",
}

// adventure is the one Adventure the server loads until the Forge delivers
// real ones: the arena, played by the two Fighters.
var adventure = lobby.Adventure{ID: "arena", Name: "Goblin arena", Characters: []string{"fighter-1", "fighter-2"}}

// games are the running Campaigns' tables, by Campaign ID.
type games struct {
	mu     sync.Mutex
	tables map[string]*table.Table
	owners map[string]map[string]string // Campaign ID -> Player Character -> Player
}

func (g *games) start(c lobby.Campaign, seed uint64) {
	cs := creatures()
	tb, err := table.New(table.Config{
		Rows:      arena,
		Creatures: cs,
		Seed:      seed,
		Brain:     tactics.NewUtility(cs, nil), // every goblin Balanced
	})
	if err != nil {
		log.Printf("dndboard: starting Campaign %s: %v", c.ID, err)
		return
	}
	go tb.Run(context.Background(), time.Second) // Reaction Prompts time out
	g.mu.Lock()
	g.tables[c.ID], g.owners[c.ID] = tb, c.Owners
	g.mu.Unlock()
}

// play seats an authenticated Player at the table of their Campaign, as a
// Player Character they control: /play?campaign=ID&pc=fighter-1.
func (g *games) play(l *lobby.Lobby) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		player, err := l.AuthenticateRequest(r)
		id, pc := r.URL.Query().Get("campaign"), r.URL.Query().Get("pc")
		g.mu.Lock()
		tb, owner := g.tables[id], g.owners[id][pc]
		g.mu.Unlock()
		if err != nil || tb == nil || owner != player {
			http.Error(w, "not logged in, or not your Player Character", http.StatusUnauthorized)
			return
		}
		ws.Handler(tb, nil).ServeHTTP(w, r)
	})
}

func main() {
	addr := flag.String("addr", "127.0.0.1:8080", "listen address")
	seed := flag.Uint64("seed", 0, "dice seed (0: from the clock)")
	dev := flag.Bool("dev", false, "developer mode: allows solo Campaigns")
	flag.Parse()
	if *seed == 0 {
		*seed = uint64(time.Now().UnixNano())
	}

	g := &games{tables: map[string]*table.Table{}, owners: map[string]map[string]string{}}
	l := lobby.New(lobby.Config{
		Adventures: []lobby.Adventure{adventure},
		Dev:        *dev,
		OnStart:    func(c lobby.Campaign) { g.start(c, *seed) },
	})
	// A few requests per second per address, with a burst for the client's
	// start-up; it also slows password guessing at login.
	limit := lobby.NewRateLimiter(20, 200*time.Millisecond, nil)

	mux := http.NewServeMux()
	mux.Handle("GET /play", g.play(l))
	mux.Handle("/", l.Handler())
	srv := &http.Server{Addr: *addr, Handler: limit.Middleware(mux), ReadHeaderTimeout: 10 * time.Second}
	log.Printf("dndboard: serving http://%s (dev %v, seed %d)", *addr, *dev, *seed)
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

func creatures() []session.CreatureSpec {
	var cs []session.CreatureSpec
	for i, p := range []board.Pos{{X: 2, Y: 3}, {X: 2, Y: 4}} {
		f := srd.Fighter()
		f.ID, f.Name, f.Pos = session.ID(fmt.Sprintf("fighter-%d", i+1)), fmt.Sprintf("Fighter %d", i+1), p
		f.PlayerCharacter = true // played from the clients; Dying at 0 Hit Points
		// The glossary's default for a Player Character is to ask (a Reaction
		// Prompt), but the client cannot answer prompts until #16; asking now
		// would only ever time out, so the demo Fighters always react.
		f.Reactions = session.ReactAlways
		cs = append(cs, f)
	}
	for i, p := range []board.Pos{{X: 11, Y: 1}, {X: 12, Y: 5}, {X: 9, Y: 6}} {
		g := srd.GoblinWarrior()
		g.ID, g.Name, g.Pos = session.ID(fmt.Sprintf("goblin-%d", i+1)), fmt.Sprintf("Goblin Warrior %d", i+1), p
		g.Group = "goblins" // identical creatures share one Initiative roll
		cs = append(cs, g)
	}
	return cs
}
