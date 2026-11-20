package ws_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/mvgv/dndboard/server/board"
	"github.com/mvgv/dndboard/server/dice"
	"github.com/mvgv/dndboard/server/session"
	"github.com/mvgv/dndboard/server/srd"
	"github.com/mvgv/dndboard/server/table"
	"github.com/mvgv/dndboard/server/ws"
)

// passive is a monster brain with no opinion: its creatures end their turns.
type passive struct{}

func (passive) Choose(session.State, session.LegalActions) (session.Intent, bool) { return nil, false }

// serve starts a table with two Fighters (Player Characters) and a goblin,
// and returns its WebSocket URL without the pc parameter.
func serve(t *testing.T) string {
	t.Helper()
	var cs []session.CreatureSpec
	for i, id := range []session.ID{"fighter", "ally"} {
		f := srd.Fighter()
		f.ID, f.Pos, f.PlayerCharacter = id, board.Pos{X: 1, Y: 1 + i}, true
		cs = append(cs, f)
	}
	g := srd.GoblinWarrior()
	g.ID, g.Pos = "goblin", board.Pos{X: 5, Y: 1}
	tb, err := table.New(table.Config{
		Rows:      []string{"#######", "#.....#", "#.....#", "#######"},
		Creatures: append(cs, g),
		Dice:      dice.Fixed(20, 19, 1), // fighter, ally, goblin
		Brain:     passive{},
	})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(ws.Handler(tb))
	t.Cleanup(srv.Close)
	return "ws" + strings.TrimPrefix(srv.URL, "http")
}

func TestJoiningNeedsAPlayerCharacter(t *testing.T) {
	url := serve(t)
	for _, q := range []string{"", "?pc=goblin", "?pc=nobody"} {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		c, resp, err := websocket.Dial(ctx, url+q, nil)
		cancel()
		if err == nil {
			c.CloseNow()
			t.Fatalf("joined with %q", q)
		}
		if resp == nil || resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("%q: response %v, want 400", q, resp)
		}
	}
}

func dial(t *testing.T, url string) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close(websocket.StatusNormalClosure, "") })
	return c
}

func read(t *testing.T, c *websocket.Conn) table.Message {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var m table.Message
	if err := wsjson.Read(ctx, c, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func send(t *testing.T, c *websocket.Conn, text string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.Write(ctx, websocket.MessageText, []byte(text)); err != nil {
		t.Fatal(err)
	}
}

// AC: moving from a client resolves on the server and updates every client.
func TestIntentOverWebSocketUpdatesEveryClient(t *testing.T) {
	url := serve(t)
	a, b := dial(t, url+"?pc=fighter"), dial(t, url+"?pc=ally")
	for _, c := range []*websocket.Conn{a, b} {
		if w := read(t, c); w.Type != "welcome" || w.Board == nil || w.State == nil {
			t.Fatalf("welcome = %+v", w)
		}
	}

	send(t, a, `{"type":"intent","intent":"move","actor":"fighter","to":{"x":4,"y":1}}`)
	for _, c := range []*websocket.Conn{a, b} {
		u := read(t, c)
		if u.Type != "update" || len(u.Events) == 0 || u.Events[0].Type != "moved" || u.Events[0].To.X != 4 {
			t.Fatalf("update = %+v", u)
		}
	}
}

func TestBadMessagesAreRejectedToTheSenderOnly(t *testing.T) {
	url := serve(t)
	a, b := dial(t, url+"?pc=fighter"), dial(t, url+"?pc=ally")
	read(t, a)
	read(t, b)

	for text, code := range map[string]string{
		`not json`:        "BadMessage",
		`{"type":"chat"}`: "BadMessage",
		`{"type":"intent","intent":"end_turn","actor":"goblin"}`:                  "NotYourTurn",
		`{"type":"intent","intent":"end_turn","actor":"ally"}`:                    "NotYourTurn",
		`{"type":"intent","intent":"attack","actor":"fighter","target":"goblin"}`: "OutOfReach",
	} {
		send(t, a, text)
		if r := read(t, a); r.Type != "rejected" || r.Code != code || r.Reason == "" {
			t.Fatalf("%s: got %+v, want rejected %s", text, r, code)
		}
	}

	// b saw none of it: its next message is the update for a legal intent.
	send(t, a, `{"type":"intent","intent":"end_turn","actor":"fighter"}`)
	if u := read(t, b); u.Type != "update" {
		t.Fatalf("b got %+v, want the update", u)
	}
}
