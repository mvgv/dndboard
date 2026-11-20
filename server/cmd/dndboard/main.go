// Command dndboard serves one cooperative combat on a hand-built Board to the
// Godot client over WebSocket: two Fighters, each played by the client that
// connects as it (ws://host/play?pc=fighter-1 or ?pc=fighter-2), against three
// Goblin Warriors run by the deterministic tactical brain.
//
// There is no login yet (#14), so it listens on localhost by default.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/mvgv/dndboard/server/board"
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

func main() {
	addr := flag.String("addr", "127.0.0.1:8080", "listen address")
	seed := flag.Uint64("seed", 0, "dice seed (0: from the clock)")
	flag.Parse()
	if *seed == 0 {
		*seed = uint64(time.Now().UnixNano())
	}

	cs := creatures()
	tb, err := table.New(table.Config{
		Rows:      arena,
		Creatures: cs,
		Seed:      *seed,
		Brain:     tactics.NewUtility(cs, nil), // every goblin Balanced
	})
	if err != nil {
		log.Fatal(err)
	}
	go tb.Run(context.Background(), time.Second) // Reaction Prompts time out

	mux := http.NewServeMux()
	mux.Handle("GET /play", ws.Handler(tb))
	srv := &http.Server{Addr: *addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	log.Printf("dndboard: serving ws://%s/play (seed %d)", *addr, *seed)
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
