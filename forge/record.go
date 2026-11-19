package forge

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
)

// Recording is one recorded Claude exchange: the exact request body and the
// response body the API returned. Tests replay recordings so they never call
// the live API, and a changed prompt or source text no longer matches any
// recording, which fails loudly instead of testing a stale answer.
type Recording struct {
	Request  json.RawMessage `json:"request"`
	Response json.RawMessage `json:"response"`
}

func requestKey(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:8])
}

// Replay is an http.RoundTripper that answers from the recordings in Dir,
// matched by the request body, and never touches the network.
type Replay struct{ Dir string }

func (r Replay) RoundTrip(req *http.Request) (*http.Response, error) {
	body, err := readBody(req)
	if err != nil {
		return nil, err
	}
	key := requestKey(body)
	data, err := os.ReadFile(filepath.Join(r.Dir, key+".json"))
	if err != nil {
		return nil, fmt.Errorf("forge: no recorded response for request %s in %s (prompt or source text changed? re-record with -record): %w", key, r.Dir, err)
	}
	var rec Recording
	if err := json.Unmarshal(data, &rec); err != nil {
		return nil, fmt.Errorf("forge: recording %s: %w", key, err)
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": {"application/json"}},
		Body:       io.NopCloser(bytes.NewReader(rec.Response)),
		Request:    req,
	}, nil
}

// Record is an http.RoundTripper that forwards to Next (the live API) and
// saves every successful exchange into Dir for Replay.
type Record struct {
	Dir  string
	Next http.RoundTripper // http.DefaultTransport when nil
}

func (r Record) RoundTrip(req *http.Request) (*http.Response, error) {
	body, err := readBody(req)
	if err != nil {
		return nil, err
	}
	next := r.Next
	if next == nil {
		next = http.DefaultTransport
	}
	resp, err := next.RoundTrip(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		return resp, err
	}
	respBody, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return nil, err
	}
	resp.Body = io.NopCloser(bytes.NewReader(respBody))

	data, err := json.MarshalIndent(Recording{Request: body, Response: respBody}, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(r.Dir, 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(r.Dir, requestKey(body)+".json"), append(data, '\n'), 0o644); err != nil {
		return nil, err
	}
	return resp, nil
}

func readBody(req *http.Request) ([]byte, error) {
	if req.Body == nil {
		return nil, nil
	}
	body, err := io.ReadAll(req.Body)
	req.Body.Close()
	if err != nil {
		return nil, err
	}
	req.Body = io.NopCloser(bytes.NewReader(body))
	return body, nil
}
