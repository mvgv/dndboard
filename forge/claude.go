package forge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// Extraction is what Claude reads out of one stat block's text, in the shape
// of the record_stat_block tool's input. It covers what content format v0
// can hold: the core numbers and one melee weapon attack. Everything else in
// the stat block is listed by name in Omitted so the reviewer sees it.
type Extraction struct {
	Name          string           `json:"name"`
	AC            int              `json:"ac"`
	HP            int              `json:"hp"`
	SpeedFt       int              `json:"speed_ft"`
	InitiativeMod int              `json:"initiative_mod"`
	Attack        ExtractionAttack `json:"attack"`
	Omitted       []string         `json:"omitted"`
}

// ExtractionAttack is the stat block's first melee weapon attack.
type ExtractionAttack struct {
	Name        string `json:"name"`
	ToHit       int    `json:"to_hit"`
	ReachFt     int    `json:"reach_ft"`
	DiceCount   int    `json:"dice_count"`
	DiceSides   int    `json:"dice_sides"`
	DamageBonus int    `json:"damage_bonus"`
	DamageType  string `json:"damage_type"`
}

// DefaultModel is the Claude model the Forge uses unless told otherwise. The
// spec leaves the per-function model to be chosen later by measured cost and
// quality.
const DefaultModel = "claude-opus-5-5"

// Claude extracts stat blocks through the Anthropic Messages API, forcing a
// single tool call whose input schema is the extraction: Claude's answer is
// always structured JSON, never prose to parse.
type Claude struct {
	APIKey  string
	Model   string       // DefaultModel when empty
	BaseURL string       // https://api.anthropic.com when empty
	HTTP    *http.Client // http.DefaultClient when nil; tests swap its Transport for recorded responses
}

const toolName = "record_stat_block"

const systemPrompt = `You read one monster stat block from the D&D System Reference Document 5.2.1 and record it with the record_stat_block tool.

The text was extracted from a two-column PDF: lines of a neighbouring stat block can appear inside it, and minus signs can come out garbled. Record only the stat block whose name is given, and copy values exactly as printed; when a value is unreadable, derive it from the rest of the stat block (Initiative is the Dexterity modifier unless the block says otherwise).

attack is the first melee weapon attack under Actions ("Melee Attack Roll" or "Melee or Ranged Attack Roll"), with its damage dice and flat bonus from the Hit line. Ignore riders such as extra damage "if the attack roll had Advantage".

omitted lists, by name, everything in the stat block that these fields don't capture and that affects play: other actions and attacks, damage riders, traits, bonus actions, reactions, other speeds, vulnerabilities, resistances and immunities. Skills, senses, languages, gear and CR don't go in omitted.`

var toolSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "name": {"type": "string", "description": "The stat block's name, as printed"},
    "ac": {"type": "integer", "description": "Armor Class"},
    "hp": {"type": "integer", "description": "Hit Points (the fixed number, not the dice)"},
    "speed_ft": {"type": "integer", "description": "Walking Speed in feet"},
    "initiative_mod": {"type": "integer", "description": "Initiative modifier, e.g. -2 for 'Initiative −2 (8)'"},
    "attack": {
      "type": "object",
      "properties": {
        "name": {"type": "string", "description": "Attack name, e.g. Scimitar"},
        "to_hit": {"type": "integer", "description": "Attack roll bonus"},
        "reach_ft": {"type": "integer"},
        "dice_count": {"type": "integer"},
        "dice_sides": {"type": "integer"},
        "damage_bonus": {"type": "integer", "description": "Flat bonus added to the damage dice, 0 if none"},
        "damage_type": {"type": "string", "description": "Capitalized, e.g. Slashing"}
      },
      "required": ["name", "to_hit", "reach_ft", "dice_count", "dice_sides", "damage_bonus", "damage_type"],
      "additionalProperties": false
    },
    "omitted": {"type": "array", "items": {"type": "string"}}
  },
  "required": ["name", "ac", "hp", "speed_ft", "initiative_mod", "attack", "omitted"],
  "additionalProperties": false
}`)

type (
	apiTool struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		InputSchema json.RawMessage `json:"input_schema"`
	}
	apiMessage struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	apiRequest struct {
		Model      string            `json:"model"`
		MaxTokens  int               `json:"max_tokens"`
		System     string            `json:"system"`
		Tools      []apiTool         `json:"tools"`
		ToolChoice map[string]string `json:"tool_choice"`
		Messages   []apiMessage      `json:"messages"`
	}
	apiResponse struct {
		Content []struct {
			Type  string          `json:"type"`
			Name  string          `json:"name"`
			Input json.RawMessage `json:"input"`
		} `json:"content"`
		StopReason string `json:"stop_reason"`
	}
	apiError struct {
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
)

// ExtractStatBlock asks Claude for the structured fields of one stat block.
func (c *Claude) ExtractStatBlock(ctx context.Context, ex Excerpt) (Extraction, error) {
	model := c.Model
	if model == "" {
		model = DefaultModel
	}
	body, err := json.Marshal(apiRequest{
		Model:     model,
		MaxTokens: 1024,
		System:    systemPrompt,
		Tools: []apiTool{{
			Name:        toolName,
			Description: "Record the fields of one stat block.",
			InputSchema: toolSchema,
		}},
		ToolChoice: map[string]string{"type": "tool", "name": toolName},
		Messages: []apiMessage{{
			Role:    "user",
			Content: fmt.Sprintf("Stat block: %s\n\n<source>\n%s</source>", ex.Name, ex.Text),
		}},
	})
	if err != nil {
		return Extraction{}, err
	}

	base := c.BaseURL
	if base == "" {
		base = "https://api.anthropic.com"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return Extraction{}, err
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("x-api-key", c.APIKey)

	hc := c.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		return Extraction{}, fmt.Errorf("forge: claude: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Extraction{}, fmt.Errorf("forge: claude: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		var e apiError
		_ = json.Unmarshal(data, &e)
		return Extraction{}, fmt.Errorf("forge: claude: HTTP %d %s: %s", resp.StatusCode, e.Error.Type, e.Error.Message)
	}
	return parseResponse(data)
}

func parseResponse(data []byte) (Extraction, error) {
	var r apiResponse
	if err := json.Unmarshal(data, &r); err != nil {
		return Extraction{}, fmt.Errorf("forge: claude: decode response: %w", err)
	}
	for _, block := range r.Content {
		if block.Type != "tool_use" || block.Name != toolName {
			continue
		}
		var x Extraction
		dec := json.NewDecoder(bytes.NewReader(block.Input))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&x); err != nil {
			return Extraction{}, fmt.Errorf("forge: claude: tool input: %w", err)
		}
		return x, nil
	}
	if r.StopReason == "max_tokens" {
		return Extraction{}, errors.New("forge: claude: response cut off at max_tokens")
	}
	return Extraction{}, fmt.Errorf("forge: claude: no %s call in response (stop_reason %q)", toolName, r.StopReason)
}
