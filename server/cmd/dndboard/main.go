// Command dndboard serves one cooperative combat on a hand-built Board to the
// Godot client over WebSocket: two Fighters, played by whoever connects,
// against three Goblin Warriors.
//
// There is no login yet (#14), so it listens on localhost by default.
package main

import (
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

	tb, err := table.New(table.Config{
		Rows:      arena,
		Creatures: creatures(),
		Seed:      *seed,
		Brain:     table.Approach{},
	})
	if err != nil {
		log.Fatal(err)
	}

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
