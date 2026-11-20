// Package ws serves a table to clients over WebSocket, as JSON text messages
// in the table's wire protocol.
package ws

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/mvgv/dndboard/server/session"
	"github.com/mvgv/dndboard/server/table"
)

const (
	// readLimit bounds one client message; intents are tiny.
	readLimit = 4 << 10
	// writeTimeout bounds one message to a client.
	writeTimeout = 10 * time.Second
)

// Handler upgrades each request to a WebSocket and seats it at the table as
// the Player Character named by the "pc" query parameter; a request that names
// none is refused with 400 before the upgrade. A request from a browser page
// on another origin is refused. When authorize is not nil, a request it
// refuses for the named Player Character gets 401 before anything else, so an
// unauthenticated client never joins; nil lets anyone play any character.
func Handler(t *table.Table, authorize func(r *http.Request, pc string) bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		viewer := r.URL.Query().Get("pc")
		if authorize != nil && !authorize(r, viewer) {
			http.Error(w, "not logged in, or not your Player Character", http.StatusUnauthorized)
			return
		}
		welcome, updates, leave, err := t.Join(viewer)
		if err != nil {
			http.Error(w, "the pc parameter must name a Player Character", http.StatusBadRequest)
			return
		}
		defer leave()
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return // Accept has written the HTTP error
		}
		defer c.CloseNow()
		c.SetReadLimit(readLimit)
		serve(r.Context(), c, t, viewer, welcome, updates)
	})
}

func serve(ctx context.Context, c *websocket.Conn, t *table.Table, viewer string,
	welcome table.Message, updates <-chan table.Message) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// replies carries rejections back to this client only.
	replies := make(chan table.Message, 8)
	go func() {
		defer cancel()
		if write(ctx, c, welcome) != nil {
			return
		}
		for {
			var m table.Message
			select {
			case <-ctx.Done():
				return
			case m = <-replies:
			case u, ok := <-updates:
				if !ok {
					c.Close(websocket.StatusPolicyViolation, "client fell behind")
					return
				}
				m = u
			}
			if write(ctx, c, m) != nil {
				return
			}
		}
	}()

	for {
		typ, data, err := c.Read(ctx)
		if err != nil {
			return
		}
		reply, ok := handle(t, viewer, typ, data)
		if !ok {
			continue
		}
		select {
		case replies <- reply:
		case <-ctx.Done():
			return
		}
	}
}

// handle submits one client message, returning the rejection to send back, if any.
func handle(t *table.Table, viewer string, typ websocket.MessageType, data []byte) (table.Message, bool) {
	var m table.ClientMessage
	if typ != websocket.MessageText || json.Unmarshal(data, &m) != nil {
		return rejected("BadMessage", "messages are JSON text"), true
	}
	if m.Type != "intent" {
		return rejected("BadMessage", "unknown message type"), true
	}
	err := t.Submit(viewer, m.Intent)
	var rej *session.Rejection
	switch {
	case err == nil:
		return table.Message{}, false
	case errors.As(err, &rej):
		return rejected(rej.Code.String(), rej.Reason), true
	}
	return rejected("Internal", "the intent could not be resolved"), true
}

func rejected(code, reason string) table.Message {
	return table.Message{Type: "rejected", Code: code, Reason: reason}
}

func write(ctx context.Context, c *websocket.Conn, m table.Message) error {
	ctx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()
	return wsjson.Write(ctx, c, m)
}
