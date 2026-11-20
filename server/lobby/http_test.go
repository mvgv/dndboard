package lobby_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/mvgv/dndboard/server/lobby"
)

func call(t *testing.T, srv *httptest.Server, method, path, token, body string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func logIn(t *testing.T, srv *httptest.Server, name string) string {
	t.Helper()
	creds := `{"name":"` + name + `","password":"correct horse"}`
	if c, b := call(t, srv, "POST", "/register", "", creds); c != 200 {
		t.Fatal(c, b)
	}
	c, b := call(t, srv, "POST", "/login", "", creds)
	var out struct{ Token string }
	if c != 200 || json.Unmarshal([]byte(b), &out) != nil || out.Token == "" {
		t.Fatal(c, b)
	}
	return out.Token
}

// AC: unauthenticated requests are refused, and requests are rate limited.
func TestHTTPNeedsLoginAndIsRateLimited(t *testing.T) {
	l, _ := newLobby(t, false)
	rl := lobby.NewRateLimiter(30, time.Hour, nil)
	srv := httptest.NewServer(rl.Middleware(l.Handler()))
	defer srv.Close()

	routes := [][2]string{{"GET", "/adventures"}, {"POST", "/campaigns"}, {"POST", "/campaigns/join"},
		{"GET", "/campaigns/x"}, {"POST", "/campaigns/solo"}}
	for _, p := range routes {
		for _, tok := range []string{"", "bogus"} {
			if c, _ := call(t, srv, p[0], p[1], tok, `{}`); c != http.StatusUnauthorized {
				t.Fatalf("%v with token %q: %d", p, tok, c)
			}
		}
	}
	if c, _ := call(t, srv, "POST", "/login", "", `{"name":"x","password":"y"}`); c != http.StatusUnauthorized {
		t.Fatalf("bad login: %d", c)
	}

	gone := logIn(t, srv, "gone")
	if c, _ := call(t, srv, "POST", "/logout", gone, ""); c != 200 {
		t.Fatalf("logout: %d", c)
	}
	if c, _ := call(t, srv, "GET", "/adventures", gone, ""); c != http.StatusUnauthorized {
		t.Fatalf("token works after logout: %d", c)
	}

	tok := logIn(t, srv, "ana")
	if c, _ := call(t, srv, "POST", "/campaigns/solo", tok, `{"adventure":"goblins"}`); c != http.StatusForbidden {
		t.Fatalf("solo: %d", c)
	}
	limited := false
	for range 60 {
		if c, _ := call(t, srv, "GET", "/adventures", tok, ""); c == http.StatusTooManyRequests {
			limited = true
			break
		}
	}
	if !limited {
		t.Fatal("never rate limited")
	}
}

// The whole flow over HTTP, with chat over WebSocket.
func TestHTTPCampaignFlowAndChat(t *testing.T) {
	l, started := newLobby(t, false)
	srv := httptest.NewServer(l.Handler())
	defer srv.Close()
	ana, bo := logIn(t, srv, "ana"), logIn(t, srv, "bo")

	c, b := call(t, srv, "POST", "/campaigns", ana, `{"adventure":"goblins"}`)
	var camp struct{ ID, Invite string }
	if c != 200 || json.Unmarshal([]byte(b), &camp) != nil || camp.Invite == "" {
		t.Fatal(c, b)
	}
	if c, b := call(t, srv, "POST", "/campaigns/join", bo, `{"invite":"`+camp.Invite+`"}`); c != 200 {
		t.Fatal(c, b)
	}
	if c, _ := call(t, srv, "GET", "/campaigns/"+camp.ID, logIn(t, srv, "cy"), ""); c != http.StatusNotFound {
		t.Fatalf("a stranger read the Campaign: %d", c)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	chatURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/campaigns/" + camp.ID + "/chat"
	dial := func(tok string) *websocket.Conn {
		ws, _, err := websocket.Dial(ctx, chatURL,
			&websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {"Bearer " + tok}}})
		if err != nil {
			t.Fatal(err)
		}
		return ws
	}
	wa, wb := dial(ana), dial(bo)
	defer wa.CloseNow()
	defer wb.CloseNow()
	if _, _, err := websocket.Dial(ctx, chatURL, nil); err == nil {
		t.Fatal("chat without login")
	}
	// Dial returns once the upgrade is done, which is after Subscribe, so the
	// first message already reaches both.
	if err := wb.Write(ctx, websocket.MessageText, []byte("oi")); err != nil {
		t.Fatal(err)
	}
	for _, ws := range []*websocket.Conn{wa, wb} {
		_, data, err := ws.Read(ctx)
		if err != nil || !strings.Contains(string(data), `"from":"bo"`) || !strings.Contains(string(data), `"text":"oi"`) {
			t.Fatalf("chat = %s, %v", data, err)
		}
	}

	for _, p := range []struct{ tok, pc string }{{ana, "pc-1"}, {ana, "pc-2"}, {bo, "pc-3"}} {
		if c, b := call(t, srv, "POST", "/campaigns/"+camp.ID+"/claim", p.tok, `{"pc":"`+p.pc+`"}`); c != 200 {
			t.Fatal(c, b)
		}
	}
	for _, tok := range []string{ana, bo} {
		if c, b := call(t, srv, "POST", "/campaigns/"+camp.ID+"/ready", tok, ""); c != 200 {
			t.Fatal(c, b)
		}
	}
	if len(*started) != 1 {
		t.Fatalf("started %d times", len(*started))
	}
}
