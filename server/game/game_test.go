package game_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/mvgv/dndboard/server/board"
	"github.com/mvgv/dndboard/server/dice"
	"github.com/mvgv/dndboard/server/game"
	"github.com/mvgv/dndboard/server/lobby"
	"github.com/mvgv/dndboard/server/session"
	"github.com/mvgv/dndboard/server/srd"
	"github.com/mvgv/dndboard/server/table"
)

type passive struct{}

func (passive) Choose(session.State, session.LegalActions) (session.Intent, bool) { return nil, false }

func newTable(lobby.Campaign) (*table.Table, error) {
	var cs []session.CreatureSpec
	for i, id := range []session.ID{"pc-1", "pc-2"} {
		f := srd.Fighter()
		f.ID, f.Pos, f.PlayerCharacter = id, board.Pos{X: 1, Y: 1 + i}, true
		cs = append(cs, f)
	}
	g := srd.GoblinWarrior()
	g.ID, g.Pos = "goblin", board.Pos{X: 5, Y: 1}
	return table.New(table.Config{
		Rows:      []string{"#######", "#.....#", "#.....#", "#######"},
		Creatures: append(cs, g),
		Dice:      dice.Fixed(20, 19, 1),
		Brain:     passive{},
	})
}

// setup runs the real lobby, starts a Campaign through it and serves /play.
func setup(t *testing.T, cfg game.Config) (srv *httptest.Server, tokens map[string]string, id string, games *game.Games) {
	t.Helper()
	cfg.NewTable = newTable
	games = game.New(cfg)
	l := lobby.New(lobby.Config{
		Adventures: []lobby.Adventure{{ID: "a", Name: "A", Characters: []string{"pc-1", "pc-2"}}},
		OnStart:    games.Start,
	})
	tokens = map[string]string{}
	for _, name := range []string{"ana", "bo", "cy"} {
		if err := l.Register(name, "correct horse"); err != nil {
			t.Fatal(err)
		}
		tok, err := l.Login(name, "correct horse")
		if err != nil {
			t.Fatal(err)
		}
		tokens[name] = tok
	}
	c, err := l.Create("ana", "a")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.Join(c.Invite, "bo"); err != nil {
		t.Fatal(err)
	}
	for _, cl := range [][2]string{{"ana", "pc-1"}, {"bo", "pc-2"}} {
		if err := l.Claim(c.ID, cl[0], cl[1]); err != nil {
			t.Fatal(err)
		}
	}
	for _, p := range []string{"ana", "bo"} {
		if err := l.Ready(c.ID, p); err != nil {
			t.Fatal(err)
		}
	}
	srv = httptest.NewServer(games.Handler(l))
	t.Cleanup(srv.Close)
	return srv, tokens, c.ID, games
}

func dial(srv *httptest.Server, token, campaign, pc string) (*http.Response, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	h := http.Header{}
	if token != "" {
		h.Set("Authorization", "Bearer "+token)
	}
	c, resp, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"?campaign="+campaign+"&pc="+pc,
		&websocket.DialOptions{HTTPHeader: h})
	if err == nil {
		c.CloseNow()
	}
	return resp, err
}

// AC: unauthenticated clients can't join; a Player plays only the characters
// they control, through lobby -> start -> /play.
func TestOnlyAPlayerCharactersOwnerGetsTheTable(t *testing.T) {
	srv, tok, id, games := setup(t, game.Config{})
	if games.Running() != 1 {
		t.Fatalf("running = %d: starting the Campaign did not open its table", games.Running())
	}
	if _, err := dial(srv, tok["ana"], id, "pc-1"); err != nil {
		t.Fatalf("owner refused: %v", err)
	}
	if _, err := dial(srv, tok["bo"], id, "pc-2"); err != nil {
		t.Fatalf("owner refused: %v", err)
	}
	for name, c := range map[string]struct{ tok, campaign, pc string }{
		"non-owner":        {tok["bo"], id, "pc-1"},
		"stranger":         {tok["cy"], id, "pc-1"},
		"no login":         {"", id, "pc-1"},
		"bad token":        {"bogus", id, "pc-1"},
		"unknown campaign": {tok["ana"], "nope", "pc-1"},
		"unknown pc":       {tok["ana"], id, "goblin"},
		"no pc":            {tok["ana"], id, ""},
	} {
		resp, err := dial(srv, c.tok, c.campaign, c.pc)
		if err == nil || resp == nil || resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s: got %v, %v; want 401", name, resp, err)
		}
	}
}

// A table is closed when its lifetime ends, and no more than Max run at once.
func TestTablesAreBounded(t *testing.T) {
	srv, tok, id, games := setup(t, game.Config{Lifetime: 50 * time.Millisecond})
	deadline := time.Now().Add(3 * time.Second)
	for games.Running() != 0 {
		if time.Now().After(deadline) {
			t.Fatal("the table never closed")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := dial(srv, tok["ana"], id, "pc-1"); err == nil {
		t.Fatal("joined a closed table")
	}

	games = game.New(game.Config{NewTable: newTable, Max: 1})
	games.Start(lobby.Campaign{ID: "one"})
	games.Start(lobby.Campaign{ID: "two"})
	if games.Running() != 1 {
		t.Fatalf("running = %d with Max 1", games.Running())
	}
}
