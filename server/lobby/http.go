package lobby

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

// bodyLimit bounds a request body; every request is a few short strings.
const bodyLimit = 4 << 10

// Handler serves the lobby over HTTP and WebSocket, every route behind login
// except /register and /login. A session token travels in the Authorization
// header ("Bearer <token>"), never in a URL.
//
//	POST /register            {name, password}
//	POST /login               {name, password}        -> {token}
//	POST /logout
//	GET  /adventures
//	POST /campaigns           {adventure}             -> Campaign
//	POST /campaigns/solo      {adventure}             (developer setting only)
//	POST /campaigns/join      {invite}                -> Campaign
//	GET  /campaigns/{id}
//	POST /campaigns/{id}/claim|release  {pc}
//	POST /campaigns/{id}/ready
//	GET  /campaigns/{id}/chat   WebSocket: text in, {from, text} out
func (l *Lobby) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /register", func(w http.ResponseWriter, r *http.Request) {
		var b struct{ Name, Password string }
		if !decode(w, r, &b) {
			return
		}
		reply(w, nil, l.Register(b.Name, b.Password))
	})
	mux.HandleFunc("POST /login", func(w http.ResponseWriter, r *http.Request) {
		var b struct{ Name, Password string }
		if !decode(w, r, &b) {
			return
		}
		tok, err := l.Login(b.Name, b.Password)
		reply(w, map[string]string{"token": tok}, err)
	})
	mux.HandleFunc("POST /logout", func(w http.ResponseWriter, r *http.Request) {
		if tok, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok {
			l.Logout(tok)
		}
		reply(w, nil, nil)
	})
	mux.HandleFunc("GET /adventures", l.authed(func(w http.ResponseWriter, r *http.Request, _ string) {
		type adv struct {
			ID         string   `json:"id"`
			Name       string   `json:"name"`
			Characters []string `json:"characters"`
		}
		var out []adv
		for _, a := range l.Adventures() {
			out = append(out, adv{a.ID, a.Name, a.Characters})
		}
		reply(w, out, nil)
	}))
	create := func(f func(player, adventure string) (Campaign, error)) http.HandlerFunc {
		return l.authed(func(w http.ResponseWriter, r *http.Request, p string) {
			var b struct{ Adventure string }
			if !decode(w, r, &b) {
				return
			}
			c, err := f(p, b.Adventure)
			reply(w, wire(c), err)
		})
	}
	mux.HandleFunc("POST /campaigns", create(l.Create))
	mux.HandleFunc("POST /campaigns/solo", create(l.Solo))
	mux.HandleFunc("POST /campaigns/join", l.authed(func(w http.ResponseWriter, r *http.Request, p string) {
		var b struct{ Invite string }
		if !decode(w, r, &b) {
			return
		}
		c, err := l.Join(b.Invite, p)
		reply(w, wire(c), err)
	}))
	mux.HandleFunc("GET /campaigns/{id}", l.authed(func(w http.ResponseWriter, r *http.Request, p string) {
		c, err := l.Campaign(r.PathValue("id"), p)
		reply(w, wire(c), err)
	}))
	pcOp := func(f func(id, player, pc string) error) http.HandlerFunc {
		return l.authed(func(w http.ResponseWriter, r *http.Request, p string) {
			var b struct{ PC string }
			if !decode(w, r, &b) {
				return
			}
			reply(w, nil, f(r.PathValue("id"), p, b.PC))
		})
	}
	mux.HandleFunc("POST /campaigns/{id}/claim", pcOp(l.Claim))
	mux.HandleFunc("POST /campaigns/{id}/release", pcOp(l.Release))
	mux.HandleFunc("POST /campaigns/{id}/ready", l.authed(func(w http.ResponseWriter, r *http.Request, p string) {
		reply(w, nil, l.Ready(r.PathValue("id"), p))
	}))
	mux.HandleFunc("GET /campaigns/{id}/chat", l.authed(l.chat))
	return http.MaxBytesHandler(mux, bodyLimit)
}

// AuthenticateRequest returns the Player making r, from its Authorization header.
func (l *Lobby) AuthenticateRequest(r *http.Request) (string, error) {
	tok, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok {
		return "", ErrUnauthenticated
	}
	return l.Authenticate(tok)
}

func (l *Lobby) authed(f func(http.ResponseWriter, *http.Request, string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, err := l.AuthenticateRequest(r)
		if err != nil {
			http.Error(w, "log in first", http.StatusUnauthorized)
			return
		}
		f(w, r, p)
	}
}

func (l *Lobby) chat(w http.ResponseWriter, r *http.Request, player string) {
	id := r.PathValue("id")
	ch, leave, err := l.Subscribe(id, player)
	if err != nil {
		reply(w, nil, err)
		return
	}
	defer leave()
	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer c.CloseNow()
	c.SetReadLimit(2 * maxChat)
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	go func() {
		defer cancel()
		for {
			typ, data, err := c.Read(ctx)
			if err != nil {
				return
			}
			if typ == websocket.MessageText {
				_ = l.Chat(id, player, string(data)) // a bad message is dropped
			}
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case m, ok := <-ch:
			if !ok {
				return
			}
			if wsjson.Write(ctx, c, map[string]string{"from": m.From, "text": m.Text}) != nil {
				return
			}
		}
	}
}

type campaignJSON struct {
	ID        string            `json:"id"`
	Invite    string            `json:"invite,omitempty"`
	Adventure string            `json:"adventure"`
	Creator   string            `json:"creator"`
	State     State             `json:"state"`
	Players   []string          `json:"players"`
	Owners    map[string]string `json:"owners"`
	Ready     []string          `json:"ready"`
}

func wire(c Campaign) campaignJSON {
	return campaignJSON{c.ID, c.Invite, c.Adventure, c.Creator, c.State, c.Players, c.Owners, c.Ready}
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		http.Error(w, "bad request body", http.StatusBadRequest)
		return false
	}
	return true
}

func reply(w http.ResponseWriter, v any, err error) {
	switch {
	case errors.Is(err, ErrUnauthenticated):
		http.Error(w, "wrong name or password", http.StatusUnauthorized)
	case errors.Is(err, ErrNoSuchCampaign):
		http.Error(w, err.Error(), http.StatusNotFound)
	case errors.Is(err, ErrTooManyCampaigns), errors.Is(err, ErrTooManyAccounts):
		http.Error(w, err.Error(), http.StatusTooManyRequests)
	case errors.Is(err, ErrSoloDisabled):
		http.Error(w, err.Error(), http.StatusForbidden)
	case err != nil:
		http.Error(w, err.Error(), http.StatusBadRequest)
	default:
		w.Header().Set("Content-Type", "application/json")
		if v == nil {
			v = map[string]bool{"ok": true}
		}
		_ = json.NewEncoder(w).Encode(v)
	}
}
