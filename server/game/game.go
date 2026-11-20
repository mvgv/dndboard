// Package game runs the tables of started Campaigns and seats Players at them:
// a Player reaches a Player Character's table only with their own login and
// only for a character the lobby says they control.
package game

import (
	"context"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/mvgv/dndboard/server/lobby"
	"github.com/mvgv/dndboard/server/table"
	"github.com/mvgv/dndboard/server/ws"
)

// Authenticator names the Player making a request.
type Authenticator interface {
	AuthenticateRequest(*http.Request) (string, error)
}

// Config creates Games.
type Config struct {
	// NewTable builds the table of a starting Campaign.
	NewTable func(lobby.Campaign) (*table.Table, error)
	// Lifetime is how long a table runs before it is closed, until Campaigns
	// can end and be saved (#15). Zero means 12 hours.
	Lifetime time.Duration
	// Max is how many tables run at once; a Campaign that starts beyond it
	// is not played. Zero means 32.
	Max int
}

type running struct {
	tb     *table.Table
	owners map[string]string // Player Character -> Player
}

// Games are the running Campaigns' tables, by Campaign ID. Safe for
// concurrent use.
type Games struct {
	cfg Config
	mu  sync.Mutex
	by  map[string]running
}

// New returns Games with no table running.
func New(cfg Config) *Games {
	if cfg.Lifetime == 0 {
		cfg.Lifetime = 12 * time.Hour
	}
	if cfg.Max == 0 {
		cfg.Max = 32
	}
	return &Games{cfg: cfg, by: map[string]running{}}
}

// Start opens the table of a Campaign that has just started. Its Reaction
// Prompts time out in real time until Lifetime ends, when the table is closed
// and forgotten.
func (g *Games) Start(c lobby.Campaign) {
	g.mu.Lock()
	full := len(g.by) >= g.cfg.Max
	g.mu.Unlock()
	if full {
		log.Printf("game: Campaign %s not started: %d tables are running", c.ID, g.cfg.Max)
		return
	}
	tb, err := g.cfg.NewTable(c)
	if err != nil {
		log.Printf("game: starting Campaign %s: %v", c.ID, err)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), g.cfg.Lifetime)
	g.mu.Lock()
	g.by[c.ID] = running{tb: tb, owners: c.Owners}
	g.mu.Unlock()
	go func() {
		defer cancel()
		tb.Run(ctx, time.Second)
		g.mu.Lock()
		delete(g.by, c.ID)
		g.mu.Unlock()
	}()
}

// Running is how many tables are open.
func (g *Games) Running() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.by)
}

// Handler seats a Player at their Campaign's table as a Player Character they
// control: /play?campaign=ID&pc=fighter-1. Whatever is wrong (no login, no such
// Campaign, a character that is not theirs) is the same 401, so nothing can be
// probed. ws.Handler's authorize hook is the gate.
func (g *Games) Handler(auth Authenticator) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.mu.Lock()
		run, ok := g.by[r.URL.Query().Get("campaign")]
		g.mu.Unlock()
		if !ok {
			http.Error(w, "not logged in, or not your Player Character", http.StatusUnauthorized)
			return
		}
		ws.Handler(run.tb, func(r *http.Request, pc string) bool {
			player, err := auth.AuthenticateRequest(r)
			return err == nil && run.owners[pc] == player
		}).ServeHTTP(w, r)
	})
}
