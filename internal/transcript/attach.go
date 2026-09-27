package transcript

import (
	"encoding/json"
	"strings"
)

// Attachment types kept in Session.Pre (records before the first usage), plus two pseudo types for
// user text. Every other attachment type (hook_success, credential_org, remote_session_change,
// command_permissions, auto_mode, prompt_snapshot, unknown types) is ignored for Pre.
const (
	AttInstructions    = "instructions"
	AttSkillListing    = "skill_listing" // only isInitial == true
	AttDeferredDelta   = "deferred_tools_delta"
	AttDeferredRecord  = "deferred_tools_record"
	AttMCPInstructions = "mcp_instructions_delta"
	AttHookContext     = "hook_additional_context"
	AttEnvironment     = "environment"
	AttModel           = "model"
	AttDate            = "date"
	AttSessionContext  = "session_context"
	AttTokensReminder  = "total_tokens_reminder"
	AttAgentListing    = "agent_listing_delta"
	AttUserMeta        = "user_meta"    // pseudo: text of an isMeta user record
	AttFirstPrompt     = "first_prompt" // pseudo: first non-meta human text (at most one)
)

// ToolDef is one element of the tool snapshot.
type ToolDef struct {
	Name  string
	Bytes int // len of the raw JSON element as stored in the line (json.RawMessage); never re-marshalled
}

// Snapshot is the first prompt_snapshot attachment anywhere in the file whose tools array is
// non-empty and decodes; a snapshot that fails to decode counts as absent.
type Snapshot struct {
	SystemPromptBytes int // Σ len(systemPrompt[i]) of that same snapshot
	Tools             []ToolDef
}

// InstructionFile is one file of an instructions attachment; content is measured and dropped.
type InstructionFile struct {
	Path  string // as written
	Type  string // "User" | "Project" | "AutoMem" | any other value as written
	Bytes int    // len(content)
}

// SkillLine is one line of the initial skill_listing content (strings.Split on "\n").
type SkillLine struct {
	Name  string // matched listed skill name; "" for a listing-overhead line
	Bytes int    // len(line), newline excluded
}

// Attachment is one size-only component record seen before the first usage, in file order.
//
// Byte rules: instructions = Σ Files[i].Bytes; skill_listing = len(content); deferred_tools_delta =
// Σ len(addedNames[i]); deferred_tools_record = Σ len(raw entries[i]); mcp_instructions_delta =
// Σ len(addedBlocks[i]); hook_additional_context = len(content[0]); environment = len(raw snapshot);
// model = len(text); date = len(date); session_context = Σ over context values (decoded string
// length, raw length for non-strings); total_tokens_reminder = len(text); agent_listing_delta =
// Σ len(addedLines[i]); user_meta and first_prompt = length of the record's text.
type Attachment struct {
	Type  string            // one of the Att* constants
	Bytes int               // see the byte rules above
	Names []string          // deferred_tools_delta.addedNames; deferred_tools_record entry names; mcp_instructions_delta.addedNames (server names); skill_listing.names
	Files []InstructionFile // AttInstructions only
	Lines []SkillLine       // AttSkillListing only
}

// ToolCall summarises the tool_use blocks of one tool name over the whole file.
type ToolCall struct {
	Name   string
	Count  int    // tool_use blocks, each tool_use id counted once
	LastTS string // greatest timestamp of a record carrying such a block ("" if none had one)
}

// attachment handles one attachment record: tool snapshots and invoked skills anywhere in the
// file, component attachments only before the first usage.
func (p *parser) attachment(raw json.RawMessage) {
	var head struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(raw, &head) != nil {
		return
	}
	switch head.Type {
	case "prompt_snapshot":
		if p.s.Snapshot == nil {
			p.s.Snapshot = decodeSnapshot(raw)
		}
		return
	case "invoked_skills":
		var v struct {
			Skills []struct {
				Name string `json:"name"`
			} `json:"skills"`
		}
		if json.Unmarshal(raw, &v) == nil {
			for _, sk := range v.Skills {
				if sk.Name != "" {
					p.invoked[sk.Name] = true
				}
			}
		}
		return
	}
	if p.usageSeen {
		return
	}
	if a, ok := decodeComponent(head.Type, raw); ok {
		p.s.Pre = append(p.s.Pre, a)
	}
}

// decodeSnapshot returns the tool snapshot of a prompt_snapshot attachment, or nil when its tools
// array is empty or any part of it does not decode (every element needs a non-empty name).
func decodeSnapshot(raw json.RawMessage) *Snapshot {
	var v struct {
		SystemPrompt []string          `json:"systemPrompt"`
		Tools        []json.RawMessage `json:"tools"`
	}
	if json.Unmarshal(raw, &v) != nil || len(v.Tools) == 0 {
		return nil
	}
	snap := &Snapshot{}
	for _, sp := range v.SystemPrompt {
		snap.SystemPromptBytes += len(sp)
	}
	for _, t := range v.Tools {
		var def struct {
			Name string `json:"name"`
		}
		if json.Unmarshal(t, &def) != nil || def.Name == "" {
			return nil
		}
		snap.Tools = append(snap.Tools, ToolDef{Name: def.Name, Bytes: len(t)})
	}
	return snap
}

// decodeComponent measures one kept attachment type; ok is false for ignored types, a non-initial
// skill listing and attachments that do not decode.
func decodeComponent(typ string, raw json.RawMessage) (a Attachment, ok bool) {
	a.Type = typ
	switch typ {
	case AttInstructions:
		var v struct {
			Files []struct {
				Path    string `json:"path"`
				Type    string `json:"type"`
				Content string `json:"content"`
			} `json:"files"`
		}
		if json.Unmarshal(raw, &v) != nil {
			return a, false
		}
		for _, f := range v.Files {
			a.Files = append(a.Files, InstructionFile{Path: f.Path, Type: f.Type, Bytes: len(f.Content)})
			a.Bytes += len(f.Content)
		}
	case AttSkillListing:
		var v struct {
			IsInitial bool     `json:"isInitial"`
			Names     []string `json:"names"`
			Content   string   `json:"content"`
		}
		if json.Unmarshal(raw, &v) != nil || !v.IsInitial {
			return a, false
		}
		a.Bytes, a.Names, a.Lines = len(v.Content), v.Names, skillLines(v.Content, v.Names)
	case AttDeferredDelta:
		var v struct {
			AddedNames []string `json:"addedNames"`
		}
		if json.Unmarshal(raw, &v) != nil {
			return a, false
		}
		a.Names, a.Bytes = v.AddedNames, sumLen(v.AddedNames)
	case AttDeferredRecord:
		var v struct {
			Entries []json.RawMessage `json:"entries"`
		}
		if json.Unmarshal(raw, &v) != nil {
			return a, false
		}
		for _, e := range v.Entries {
			a.Bytes += len(e)
			var def struct {
				Name string `json:"name"`
			}
			if json.Unmarshal(e, &def) == nil && def.Name != "" {
				a.Names = append(a.Names, def.Name)
			}
		}
	case AttMCPInstructions:
		var v struct {
			AddedNames  []string `json:"addedNames"`
			AddedBlocks []string `json:"addedBlocks"`
		}
		if json.Unmarshal(raw, &v) != nil {
			return a, false
		}
		a.Names, a.Bytes = v.AddedNames, sumLen(v.AddedBlocks)
	case AttHookContext:
		var v struct {
			Content []string `json:"content"`
		}
		if json.Unmarshal(raw, &v) != nil {
			return a, false
		}
		if len(v.Content) > 0 {
			a.Bytes = len(v.Content[0])
		}
	case AttEnvironment:
		var v struct {
			Snapshot json.RawMessage `json:"snapshot"`
		}
		if json.Unmarshal(raw, &v) != nil {
			return a, false
		}
		a.Bytes = len(v.Snapshot)
	case AttModel, AttTokensReminder:
		var v struct {
			Text string `json:"text"`
		}
		if json.Unmarshal(raw, &v) != nil {
			return a, false
		}
		a.Bytes = len(v.Text)
	case AttDate:
		var v struct {
			Date string `json:"date"`
		}
		if json.Unmarshal(raw, &v) != nil {
			return a, false
		}
		a.Bytes = len(v.Date)
	case AttSessionContext:
		var v struct {
			Context map[string]json.RawMessage `json:"context"`
		}
		if json.Unmarshal(raw, &v) != nil {
			return a, false
		}
		for _, val := range v.Context {
			if s, isString := stringValue(val); isString {
				a.Bytes += len(s)
			} else {
				a.Bytes += len(val)
			}
		}
	case AttAgentListing:
		var v struct {
			AddedLines []string `json:"addedLines"`
		}
		if json.Unmarshal(raw, &v) != nil {
			return a, false
		}
		a.Bytes = sumLen(v.AddedLines)
	default:
		return a, false
	}
	return a, true
}

// skillLines splits an initial skill listing into lines and walks names forward: a line belongs
// to the first name at or after the previous match for which it is "- <name>", "- <name>:" or
// starts with "- <name>: "; every other line is listing overhead (Name ""). A listed name without
// a line of its own is skipped, and a name that prefixes another never matches the longer one.
func skillLines(content string, names []string) []SkillLine {
	parts := strings.Split(content, "\n")
	out := make([]SkillLine, 0, len(parts))
	j := 0
	for _, line := range parts {
		sl := SkillLine{Bytes: len(line)}
		for k := j; k < len(names); k++ {
			if isSkillLine(line, names[k]) {
				sl.Name, j = names[k], k+1
				break
			}
		}
		out = append(out, sl)
	}
	return out
}

// isSkillLine reports whether line is "- <name>", "- <name>:" or starts with "- <name>: ".
func isSkillLine(line, name string) bool {
	rest, ok := strings.CutPrefix(line, "- ")
	if !ok {
		return false
	}
	rest, ok = strings.CutPrefix(rest, name)
	return ok && (rest == "" || rest == ":" || strings.HasPrefix(rest, ": "))
}

func sumLen(xs []string) int {
	n := 0
	for _, x := range xs {
		n += len(x)
	}
	return n
}
