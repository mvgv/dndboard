// Package lobby is everything before the first turn: Players log in, create a
// Campaign from a loaded Adventure, invite friends with a code, claim Player
// Characters (one Player may control several), mark themselves ready and chat.
// When every Player is ready and every Player Character has a Player, the
// Campaign starts. It never touches the rules engine: starting only hands the
// Campaign (who controls which character) to Config.OnStart.
package lobby

import (
	"cmp"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"unicode/utf8"
)

var (
	ErrUnauthenticated  = errors.New("lobby: not logged in")
	ErrNoSuchCampaign   = errors.New("lobby: no such Campaign")
	ErrSoloDisabled     = errors.New("lobby: solo mode is a developer setting and is off")
	ErrTooManyAccounts  = errors.New("lobby: no more accounts can be registered")
	ErrTooManyCampaigns = errors.New("lobby: you have created as many Campaigns as allowed")
)

const (
	minPassword   = 8
	maxPassword   = 128
	maxName       = 32
	maxChat       = 500
	chatBuffer    = 64
	pbkdf2Rounds  = 100_000
	inviteLetters = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789" // no look-alikes
	inviteLength  = 8
)

// Adventure is a loaded Adventure as the lobby sees it: its name and the
// Player Characters of its Party.
type Adventure struct {
	ID         string
	Name       string
	Characters []string
}

// State of a Campaign.
type State string

const (
	Gathering State = "gathering"
	Playing   State = "playing"
)

// Campaign is a snapshot of one Campaign.
type Campaign struct {
	ID        string
	Invite    string
	Adventure string
	Creator   string
	State     State
	// Players in join order.
	Players []string
	// Owners maps each claimed Player Character to its Player.
	Owners map[string]string
	// Ready lists the Players who are ready, sorted.
	Ready []string
}

// ChatMessage is one line of in-game text chat.
type ChatMessage struct {
	From string
	Text string
}

// Config creates a Lobby.
type Config struct {
	Adventures []Adventure
	// Dev turns on solo mode; it is off unless a developer sets it.
	Dev bool
	// OnStart is called, outside the lobby's lock, when a Campaign starts. If
	// it fails, the Campaign goes back to Gathering (a solo one is dropped)
	// and the error reaches the Player whose action started it.
	OnStart func(Campaign) error
	// MaxAccounts bounds registered accounts, which live in memory (0: 1000).
	MaxAccounts int
	// MaxCampaigns is how many Campaigns one Player may create (0: 5).
	MaxCampaigns int
}

func (c Config) maxCampaigns() int {
	if c.MaxCampaigns > 0 {
		return c.MaxCampaigns
	}
	return 5
}

type campaign struct {
	Campaign
	ready map[string]bool
	subs  map[*subscriber]struct{}
}

type subscriber struct {
	player string
	ch     chan ChatMessage
}

type account struct{ salt, hash []byte }

// Lobby is safe for concurrent use.
type Lobby struct {
	cfg        Config
	adventures map[string]Adventure

	mu        sync.Mutex
	accounts  map[string]account
	tokens    map[string]string // token -> player
	campaigns map[string]*campaign
	invites   map[string]string // invite code -> campaign ID
}

// New returns an empty Lobby.
func New(cfg Config) *Lobby {
	l := &Lobby{
		cfg:        cfg,
		adventures: map[string]Adventure{},
		accounts:   map[string]account{},
		tokens:     map[string]string{},
		campaigns:  map[string]*campaign{},
		invites:    map[string]string{},
	}
	for _, a := range cfg.Adventures {
		l.adventures[a.ID] = a
	}
	return l
}

// Adventures lists the loaded Adventures.
func (l *Lobby) Adventures() []Adventure {
	out := slices.Clone(l.cfg.Adventures)
	slices.SortFunc(out, func(a, b Adventure) int { return strings.Compare(a.ID, b.ID) })
	return out
}

// ---- login ----

func hashPassword(salt []byte, password string) ([]byte, error) {
	return pbkdf2.Key(sha256.New, password, salt, pbkdf2Rounds, 32)
}

// Register creates an account.
func (l *Lobby) Register(name, password string) error {
	if n := utf8.RuneCountInString(name); n == 0 || n > maxName || strings.TrimSpace(name) != name {
		return fmt.Errorf("lobby: a name has 1 to %d characters and no surrounding spaces", maxName)
	}
	if n := len(password); n < minPassword || n > maxPassword {
		return fmt.Errorf("lobby: a password has %d to %d bytes", minPassword, maxPassword)
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return err
	}
	h, err := hashPassword(salt, password)
	if err != nil {
		return err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, ok := l.accounts[name]; ok {
		return fmt.Errorf("lobby: the name %q is taken", name)
	}
	if limit := cmp.Or(l.cfg.MaxAccounts, 1000); len(l.accounts) >= limit {
		return ErrTooManyAccounts
	}
	l.accounts[name] = account{salt: salt, hash: h}
	return nil
}

// dummySalt keeps Login's cost the same for an unknown name.
var dummySalt = make([]byte, 16)

// Login checks the password and returns a session token.
func (l *Lobby) Login(name, password string) (string, error) {
	l.mu.Lock()
	acc, ok := l.accounts[name]
	l.mu.Unlock()
	if !ok {
		acc.salt = dummySalt
	}
	h, err := hashPassword(acc.salt, password)
	if err != nil {
		return "", err
	}
	if !ok || subtle.ConstantTimeCompare(h, acc.hash) != 1 {
		return "", ErrUnauthenticated
	}
	tok, err := randomHex(32)
	if err != nil {
		return "", err
	}
	l.mu.Lock()
	l.tokens[tok] = name
	l.mu.Unlock()
	return tok, nil
}

// Logout ends a session.
func (l *Lobby) Logout(token string) {
	l.mu.Lock()
	delete(l.tokens, token)
	l.mu.Unlock()
}

// Authenticate returns the Player a token belongs to.
func (l *Lobby) Authenticate(token string) (string, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	p, ok := l.tokens[token]
	if !ok {
		return "", ErrUnauthenticated
	}
	return p, nil
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func randomInvite() (string, error) {
	b := make([]byte, inviteLength)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	for i := range b {
		b[i] = inviteLetters[int(b[i])%len(inviteLetters)]
	}
	return string(b), nil
}

// ---- campaigns ----

func (c *campaign) snapshot() Campaign {
	s := c.Campaign
	s.Players = slices.Clone(c.Players)
	s.Owners = map[string]string{}
	for k, v := range c.Owners {
		s.Owners[k] = v
	}
	s.Ready = nil
	for p := range c.ready {
		s.Ready = append(s.Ready, p)
	}
	slices.Sort(s.Ready)
	return s
}

func (l *Lobby) create(creator, adventure string) (*campaign, error) {
	a, ok := l.adventures[adventure]
	if !ok {
		return nil, fmt.Errorf("lobby: no Adventure %q is loaded", adventure)
	}
	// Campaigns do not end yet (#15), so each Player may create only so many.
	n := 0
	for _, c := range l.campaigns {
		if c.Creator == creator {
			n++
		}
	}
	if n >= l.cfg.maxCampaigns() {
		return nil, ErrTooManyCampaigns
	}
	id, err := randomHex(8)
	if err != nil {
		return nil, err
	}
	invite, err := randomInvite()
	if err != nil {
		return nil, err
	}
	c := &campaign{
		Campaign: Campaign{ID: id, Invite: invite, Adventure: a.ID, Creator: creator,
			State: Gathering, Players: []string{creator}, Owners: map[string]string{}},
		ready: map[string]bool{},
		subs:  map[*subscriber]struct{}{},
	}
	l.campaigns[id] = c
	l.invites[invite] = id
	return c, nil
}

// Create opens a Campaign from a loaded Adventure; the creator is its first
// Player and its invite code is in the result.
func (l *Lobby) Create(creator, adventure string) (Campaign, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	c, err := l.create(creator, adventure)
	if err != nil {
		return Campaign{}, err
	}
	return c.snapshot(), nil
}

// Solo starts a Campaign in which one Player controls the whole Party. It is
// a developer tool: without Config.Dev it fails with ErrSoloDisabled.
func (l *Lobby) Solo(player, adventure string) (Campaign, error) {
	if !l.cfg.Dev {
		return Campaign{}, ErrSoloDisabled
	}
	l.mu.Lock()
	c, err := l.create(player, adventure)
	if err != nil {
		l.mu.Unlock()
		return Campaign{}, err
	}
	for _, pc := range l.adventures[adventure].Characters {
		c.Owners[pc] = player
	}
	c.State = Playing
	delete(l.invites, c.Invite) // nobody joins a solo Campaign
	snap := c.snapshot()
	l.mu.Unlock()
	if err := l.started(snap); err != nil {
		l.mu.Lock()
		delete(l.campaigns, c.ID)
		l.mu.Unlock()
		return Campaign{}, err
	}
	return snap, nil
}

// Join adds a Player to the Gathering Campaign the invite code opens.
func (l *Lobby) Join(invite, player string) (Campaign, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	id, ok := l.invites[strings.ToUpper(strings.TrimSpace(invite))]
	if !ok {
		return Campaign{}, ErrNoSuchCampaign
	}
	c := l.campaigns[id]
	if c.State != Gathering {
		return Campaign{}, fmt.Errorf("lobby: the Campaign has started")
	}
	if !slices.Contains(c.Players, player) {
		c.Players = append(c.Players, player)
		clear(c.ready) // the Party changed
	}
	return c.snapshot(), nil
}

// member returns the Campaign if player takes part in it. A stranger gets
// ErrNoSuchCampaign, so Campaign IDs cannot be probed.
func (l *Lobby) member(id, player string) (*campaign, error) {
	c, ok := l.campaigns[id]
	if !ok || !slices.Contains(c.Players, player) {
		return nil, ErrNoSuchCampaign
	}
	return c, nil
}

// Campaign returns the Campaign to a Player who takes part in it.
func (l *Lobby) Campaign(id, player string) (Campaign, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	c, err := l.member(id, player)
	if err != nil {
		return Campaign{}, err
	}
	return c.snapshot(), nil
}

// Claim makes player the controller of a Player Character; a Player may claim
// several. Changing the Party withdraws everyone's readiness.
func (l *Lobby) Claim(id, player, pc string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	c, err := l.member(id, player)
	if err != nil {
		return err
	}
	if c.State != Gathering {
		return fmt.Errorf("lobby: the Campaign has started")
	}
	if !slices.Contains(l.adventures[c.Adventure].Characters, pc) {
		return fmt.Errorf("lobby: the Adventure has no Player Character %q", pc)
	}
	if owner, taken := c.Owners[pc]; taken && owner != player {
		return fmt.Errorf("lobby: %q is controlled by another Player", pc)
	}
	c.Owners[pc] = player
	clear(c.ready)
	return nil
}

// Release gives up a Player Character the Player controls.
func (l *Lobby) Release(id, player, pc string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	c, err := l.member(id, player)
	if err != nil {
		return err
	}
	if c.State != Gathering {
		return fmt.Errorf("lobby: the Campaign has started")
	}
	if c.Owners[pc] != player {
		return fmt.Errorf("lobby: you do not control %q", pc)
	}
	delete(c.Owners, pc)
	clear(c.ready)
	return nil
}

// Ready marks the Player ready. When every Player is ready, every Player
// Character has a Player and every Player has a character, the Campaign starts.
func (l *Lobby) Ready(id, player string) error {
	l.mu.Lock()
	c, err := l.member(id, player)
	if err != nil {
		l.mu.Unlock()
		return err
	}
	if c.State != Gathering {
		l.mu.Unlock()
		return fmt.Errorf("lobby: the Campaign has started")
	}
	c.ready[player] = true
	if !l.complete(c) {
		l.mu.Unlock()
		return nil
	}
	c.State = Playing
	delete(l.invites, c.Invite)
	snap := c.snapshot()
	l.mu.Unlock()
	if err := l.started(snap); err != nil {
		// The game could not open: the Players stay in the lobby and may try again.
		l.mu.Lock()
		c.State = Gathering
		l.invites[c.Invite] = c.ID
		clear(c.ready)
		l.mu.Unlock()
		return err
	}
	return nil
}

func (l *Lobby) complete(c *campaign) bool {
	if len(c.Owners) != len(l.adventures[c.Adventure].Characters) {
		return false
	}
	// One Player alone is Solo mode, which only the developer setting allows.
	if len(c.Players) < 2 && !l.cfg.Dev {
		return false
	}
	controlling := map[string]bool{}
	for _, p := range c.Owners {
		controlling[p] = true
	}
	for _, p := range c.Players {
		if !c.ready[p] || !controlling[p] {
			return false
		}
	}
	return true
}

func (l *Lobby) started(c Campaign) error {
	if l.cfg.OnStart != nil {
		return l.cfg.OnStart(c)
	}
	return nil
}

// ---- chat ----

// Subscribe returns the Campaign's chat for player, and leave, which closes
// the channel. A subscriber more than chatBuffer messages behind misses
// messages rather than stalling the others.
func (l *Lobby) Subscribe(id, player string) (<-chan ChatMessage, func(), error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	c, err := l.member(id, player)
	if err != nil {
		return nil, nil, err
	}
	s := &subscriber{player: player, ch: make(chan ChatMessage, chatBuffer)}
	c.subs[s] = struct{}{}
	return s.ch, func() {
		l.mu.Lock()
		defer l.mu.Unlock()
		if _, ok := c.subs[s]; ok {
			delete(c.subs, s)
			close(s.ch)
		}
	}, nil
}

// Chat sends a line of text to every Player in the Campaign.
func (l *Lobby) Chat(id, player, text string) error {
	text = strings.TrimSpace(text)
	if text == "" || len(text) > maxChat || !utf8.ValidString(text) {
		return fmt.Errorf("lobby: a chat message has 1 to %d bytes of text", maxChat)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	c, err := l.member(id, player)
	if err != nil {
		return err
	}
	m := ChatMessage{From: player, Text: text}
	for s := range c.subs {
		select {
		case s.ch <- m:
		default: // a client that does not read misses messages
		}
	}
	return nil
}
