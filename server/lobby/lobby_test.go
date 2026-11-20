package lobby_test

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mvgv/dndboard/server/lobby"
)

var adventure = lobby.Adventure{ID: "goblins", Name: "Goblin Ambush", Characters: []string{"pc-1", "pc-2", "pc-3"}}

func newLobby(t *testing.T, dev bool) (*lobby.Lobby, *[]lobby.Campaign) {
	t.Helper()
	var started []lobby.Campaign
	l := lobby.New(lobby.Config{
		Adventures: []lobby.Adventure{adventure},
		Dev:        dev,
		OnStart:    func(c lobby.Campaign) { started = append(started, c) },
	})
	return l, &started
}

// AC: unauthenticated clients can't join.
func TestUnauthenticatedClientsCannotJoin(t *testing.T) {
	l, _ := newLobby(t, false)
	for _, tok := range []string{"", "nope"} {
		if _, err := l.Authenticate(tok); !errors.Is(err, lobby.ErrUnauthenticated) {
			t.Fatalf("token %q: %v", tok, err)
		}
	}
	if err := l.Register("ana", "correct horse"); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Login("ana", "wrong"); !errors.Is(err, lobby.ErrUnauthenticated) {
		t.Fatalf("wrong password: %v", err)
	}
	if _, err := l.Login("nobody", "x"); !errors.Is(err, lobby.ErrUnauthenticated) {
		t.Fatalf("unknown player: %v", err)
	}
	if err := l.Register("ana", "another pass"); err == nil {
		t.Fatal("registered a taken name")
	}
	if err := l.Register("bo", "short"); err == nil {
		t.Fatal("registered a short password")
	}
	tok, err := l.Login("ana", "correct horse")
	if err != nil {
		t.Fatal(err)
	}
	if p, err := l.Authenticate(tok); err != nil || p != "ana" {
		t.Fatalf("authenticate = %q, %v", p, err)
	}
	l.Logout(tok)
	if _, err := l.Authenticate(tok); !errors.Is(err, lobby.ErrUnauthenticated) {
		t.Fatalf("token still valid after logout: %v", err)
	}
}

// AC: a creator gets an invite code; Players join with it and mark ready; the
// Campaign starts when all are ready.
func TestCampaignStartsWhenAllAreReady(t *testing.T) {
	l, started := newLobby(t, false)
	if got := l.Adventures(); len(got) != 1 || got[0].ID != "goblins" {
		t.Fatalf("adventures = %+v", got)
	}
	if _, err := l.Create("ana", "missing"); err == nil {
		t.Fatal("created a Campaign from an Adventure that is not loaded")
	}
	c, err := l.Create("ana", "goblins")
	if err != nil {
		t.Fatal(err)
	}
	if c.Invite == "" || c.Creator != "ana" || c.State != lobby.Gathering || !slices.Equal(c.Players, []string{"ana"}) {
		t.Fatalf("campaign = %+v", c)
	}
	if _, err := l.Join("WRONG", "bo"); !errors.Is(err, lobby.ErrNoSuchCampaign) {
		t.Fatalf("join with a bad code: %v", err)
	}
	if _, err := l.Join(c.Invite, "bo"); err != nil {
		t.Fatal(err)
	}
	// ana takes two characters, bo one.
	claims := [][2]string{{"ana", "pc-1"}, {"ana", "pc-2"}, {"bo", "pc-3"}}
	for _, cl := range claims {
		if err := l.Claim(c.ID, cl[0], cl[1]); err != nil {
			t.Fatal(err)
		}
	}
	if err := l.Ready(c.ID, "ana"); err != nil {
		t.Fatal(err)
	}
	if len(*started) != 0 {
		t.Fatal("started before everyone was ready")
	}
	if err := l.Ready(c.ID, "bo"); err != nil {
		t.Fatal(err)
	}
	if len(*started) != 1 {
		t.Fatalf("started %d times", len(*started))
	}
	s := (*started)[0]
	if s.State != lobby.Playing || s.Owners["pc-2"] != "ana" || s.Owners["pc-3"] != "bo" {
		t.Fatalf("started = %+v", s)
	}
	if _, err := l.Join(c.Invite, "cy"); err == nil {
		t.Fatal("joined a Campaign already playing")
	}
}

// The Campaign does not start while a character has no Player, or a Player no
// character; readiness is withdrawn when the Party changes.
func TestCampaignNeedsEveryCharacterClaimed(t *testing.T) {
	l, started := newLobby(t, false)
	c, _ := l.Create("ana", "goblins")
	l.Join(c.Invite, "bo")
	l.Claim(c.ID, "ana", "pc-1")
	l.Claim(c.ID, "ana", "pc-2")
	l.Ready(c.ID, "ana")
	l.Ready(c.ID, "bo")
	if len(*started) != 0 {
		t.Fatal("started with an unclaimed character and a Player without one")
	}
	if err := l.Claim(c.ID, "bo", "pc-1"); err == nil {
		t.Fatal("claimed a character another Player controls")
	}
	if err := l.Claim(c.ID, "bo", "pc-9"); err == nil {
		t.Fatal("claimed a character the Adventure does not have")
	}
	if err := l.Claim(c.ID, "cy", "pc-3"); err == nil {
		t.Fatal("a stranger claimed a character")
	}
	// Claiming changes the Party: readiness is cleared.
	l.Claim(c.ID, "bo", "pc-3")
	if len(*started) != 0 {
		t.Fatal("started on a claim without fresh readiness")
	}
	l.Ready(c.ID, "ana")
	l.Ready(c.ID, "bo")
	if len(*started) != 1 {
		t.Fatalf("started %d times", len(*started))
	}
	if err := l.Claim(c.ID, "bo", "pc-2"); err == nil {
		t.Fatal("claimed after the Campaign started")
	}
}

// Joining again changes nothing, but a new Player withdraws readiness.
func TestJoiningWithdrawsReadinessOnlyForANewPlayer(t *testing.T) {
	l, _ := newLobby(t, false)
	c, _ := l.Create("ana", "goblins")
	l.Claim(c.ID, "ana", "pc-1")
	l.Ready(c.ID, "ana")
	if _, err := l.Join(c.Invite, "ana"); err != nil {
		t.Fatal(err)
	}
	if got, _ := l.Campaign(c.ID, "ana"); len(got.Ready) != 1 || len(got.Players) != 1 {
		t.Fatalf("rejoining changed the Campaign: %+v", got)
	}
	l.Join(c.Invite, "bo")
	if got, _ := l.Campaign(c.ID, "ana"); len(got.Ready) != 0 || len(got.Players) != 2 {
		t.Fatalf("a new Player did not withdraw readiness: %+v", got)
	}
}

// Campaigns do not end yet, so a Player may create only so many.
func TestAPlayerCreatesALimitedNumberOfCampaigns(t *testing.T) {
	l := lobby.New(lobby.Config{Adventures: []lobby.Adventure{adventure}, MaxCampaigns: 2})
	for range 2 {
		if _, err := l.Create("ana", "goblins"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := l.Create("ana", "goblins"); !errors.Is(err, lobby.ErrTooManyCampaigns) {
		t.Fatalf("third Campaign: %v", err)
	}
	if _, err := l.Create("bo", "goblins"); err != nil {
		t.Fatalf("another Player's limit: %v", err)
	}
}

// AC: a Player can be assigned more than one Player Character.
func TestAPlayerCanControlSeveralCharacters(t *testing.T) {
	l, _ := newLobby(t, false)
	c, _ := l.Create("ana", "goblins")
	for _, pc := range adventure.Characters {
		if err := l.Claim(c.ID, "ana", pc); err != nil {
			t.Fatal(err)
		}
	}
	got, err := l.Campaign(c.ID, "ana")
	if err != nil || len(got.Owners) != 3 {
		t.Fatalf("owners = %v, %v", got.Owners, err)
	}
	if err := l.Release(c.ID, "ana", "pc-3"); err != nil {
		t.Fatal(err)
	}
	got, _ = l.Campaign(c.ID, "ana")
	if _, ok := got.Owners["pc-3"]; ok {
		t.Fatal("released character still owned")
	}
	if _, err := l.Campaign(c.ID, "cy"); err == nil {
		t.Fatal("a stranger read the Campaign")
	}
}

// AC: text chat reaches every Player in the Campaign, and only them.
func TestChatReachesEveryPlayerInTheCampaign(t *testing.T) {
	l, _ := newLobby(t, false)
	c, _ := l.Create("ana", "goblins")
	l.Join(c.Invite, "bo")
	other, _ := l.Create("cy", "goblins")

	ana, leaveA, _ := l.Subscribe(c.ID, "ana")
	defer leaveA()
	bo, leaveB, _ := l.Subscribe(c.ID, "bo")
	defer leaveB()
	cy, leaveC, _ := l.Subscribe(other.ID, "cy")
	defer leaveC()
	if _, _, err := l.Subscribe(c.ID, "cy"); err == nil {
		t.Fatal("a stranger subscribed to the chat")
	}

	if err := l.Chat(c.ID, "bo", "  olá, grupo  "); err != nil {
		t.Fatal(err)
	}
	for name, ch := range map[string]<-chan lobby.ChatMessage{"ana": ana, "bo": bo} {
		select {
		case m := <-ch:
			if m.From != "bo" || m.Text != "olá, grupo" {
				t.Fatalf("%s got %+v", name, m)
			}
		case <-time.After(time.Second):
			t.Fatalf("%s got no message", name)
		}
	}
	select {
	case m := <-cy:
		t.Fatalf("another Campaign's Player got %+v", m)
	default:
	}
	if err := l.Chat(c.ID, "cy", "hi"); err == nil {
		t.Fatal("a stranger chatted")
	}
	for _, bad := range []string{"", "   ", strings.Repeat("x", 2000)} {
		if err := l.Chat(c.ID, "ana", bad); err == nil {
			t.Fatalf("accepted chat of %d bytes", len(bad))
		}
	}
}

// AC: solo mode is available only behind a developer setting.
func TestSoloModeNeedsTheDeveloperSetting(t *testing.T) {
	l, started := newLobby(t, false)
	if _, err := l.Solo("ana", "goblins"); !errors.Is(err, lobby.ErrSoloDisabled) {
		t.Fatalf("solo without the setting: %v", err)
	}
	if len(*started) != 0 {
		t.Fatal("a solo Campaign started")
	}

	l, started = newLobby(t, true)
	c, err := l.Solo("ana", "goblins")
	if err != nil {
		t.Fatal(err)
	}
	if len(*started) != 1 || c.State != lobby.Playing || len((*started)[0].Owners) != 3 {
		t.Fatalf("solo campaign = %+v, started %d", c, len(*started))
	}
	for pc, p := range (*started)[0].Owners {
		if p != "ana" {
			t.Fatalf("%s is controlled by %s", pc, p)
		}
	}
}

// AC: requests are rate limited.
func TestRateLimiterAllowsABurstThenRefills(t *testing.T) {
	now := time.Unix(0, 0)
	rl := lobby.NewRateLimiter(2, time.Second, func() time.Time { return now })
	for range 2 {
		if !rl.Allow("a") {
			t.Fatal("burst refused")
		}
	}
	if rl.Allow("a") {
		t.Fatal("over the burst was allowed")
	}
	if !rl.Allow("b") {
		t.Fatal("one client's use limited another")
	}
	now = now.Add(1100 * time.Millisecond)
	if !rl.Allow("a") {
		t.Fatal("not refilled")
	}
	if rl.Allow("a") {
		t.Fatal("refilled more than one")
	}
}
