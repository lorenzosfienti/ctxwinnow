// Package transcript reads Claude Code session transcripts (~/.claude/projects/**/*.jsonl).
package transcript

import (
	"bufio"
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"slices"
	"strings"
	"time"
)

// EventKind tells which field of an Event is set.
type EventKind int

const (
	EvToolResult EventKind = iota
	EvUsage
	EvCompact
)

// ToolResult is one tool_result block joined with the tool_use that produced it.
type ToolResult struct {
	ToolUseID string
	ToolName  string // "?" when no matching tool_use was found in the session
	ToolInput string // raw JSON of tool_use.input
	Text      string // text blocks joined with "\n"
	NonText   bool   // content had a non-text block (image, document, ...)
	IsError   bool
}

// Usage is the token usage of one assistant message, de-duplicated by message id.
type Usage struct {
	MessageID                       string
	Input, CacheRead, CacheCreation int
}

// Context is the number of input tokens the model saw for this message.
func (u Usage) Context() int { return u.Input + u.CacheRead + u.CacheCreation }

// Event is one relevant record, in file order.
type Event struct {
	Kind   EventKind
	Result ToolResult
	Usage  Usage
}

// Session is one transcript file.
type Session struct {
	CWD             string
	FirstTS, LastTS string // earliest / latest timestamp as written in the file ("" if none), ordered as instants
	Events          []Event
	Malformed       int // non-blank lines that were not valid JSON
	Unmatched       int // tool_results without a matching tool_use

	// Size-only records for `ctxwinnow overhead`: contents are measured and dropped.
	Version            string       // first non-empty "version"
	Entrypoint         string       // first non-empty "entrypoint"
	Fork               bool         // a record of type "fork-context-ref" exists
	Snapshot           *Snapshot    // nil when no tool snapshot
	Pre                []Attachment // kept attachments and user text before the first usage
	ToolCalls          []ToolCall   // sorted by Name
	SkillCalls         []string     // Skill tool_use input.skill values, sorted unique
	Commands           []string     // <command-name> values (whole file), leading "/" stripped, sorted unique
	InvokedSkills      []string     // invoked_skills attachment skills[].name (whole file), sorted unique
	CompactBeforeUsage int          // compact_boundary records before the first usage
}

type rawRecord struct {
	Type       string          `json:"type"`
	Subtype    string          `json:"subtype"`
	CWD        string          `json:"cwd"`
	Timestamp  string          `json:"timestamp"`
	Version    string          `json:"version"`
	Entrypoint string          `json:"entrypoint"`
	IsMeta     bool            `json:"isMeta"`
	Attachment json.RawMessage `json:"attachment"`
	Message    *struct {
		ID      string          `json:"id"`
		Content json.RawMessage `json:"content"`
		Usage   *struct {
			InputTokens              int `json:"input_tokens"`
			CacheReadInputTokens     int `json:"cache_read_input_tokens"`
			CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

type rawBlock struct {
	Type      string          `json:"type"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
	IsError   bool            `json:"is_error"`
	Text      string          `json:"text"`
}

type parser struct {
	s           *Session
	uses        map[string]rawBlock
	usageAt     map[string]int // message id -> index of its usage event
	usageSeen   bool           // a usage with context > 0 was seen: Pre is closed
	firstPrompt bool           // the first prompt was recorded
	calls       map[string]*ToolCall
	skills      map[string]bool // Skill input.skill values
	commands    map[string]bool // <command-name> values
	invoked     map[string]bool // invoked_skills names
}

// Parse reads one transcript. Malformed lines are counted and skipped; only read errors are
// returned. Lines of any length are supported.
func Parse(r io.Reader) (*Session, error) {
	p := parser{s: &Session{}, uses: map[string]rawBlock{}, usageAt: map[string]int{},
		calls: map[string]*ToolCall{}, skills: map[string]bool{}, commands: map[string]bool{}, invoked: map[string]bool{}}
	br := bufio.NewReaderSize(r, 1<<20)
	for {
		line, err := br.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) > 0 {
			p.line(line)
		}
		if errors.Is(err, io.EOF) {
			p.finish()
			return p.s, nil
		}
		if err != nil {
			p.finish()
			return p.s, err
		}
	}
}

// finish turns the whole-file sets into the sorted Session fields.
func (p *parser) finish() {
	s := p.s
	for _, c := range p.calls {
		s.ToolCalls = append(s.ToolCalls, *c)
	}
	slices.SortFunc(s.ToolCalls, func(a, b ToolCall) int { return cmp.Compare(a.Name, b.Name) })
	s.SkillCalls = slices.Sorted(maps.Keys(p.skills))
	s.Commands = slices.Sorted(maps.Keys(p.commands))
	s.InvokedSkills = slices.Sorted(maps.Keys(p.invoked))
}

// tsBefore orders two transcript timestamps as instants (RFC 3339 with any fractional precision or
// offset); when either does not parse, it falls back to comparing the strings.
func tsBefore(a, b string) bool {
	ta, errA := time.Parse(time.RFC3339Nano, a)
	tb, errB := time.Parse(time.RFC3339Nano, b)
	if errA != nil || errB != nil {
		return a < b
	}
	return ta.Before(tb)
}

func (p *parser) line(line []byte) {
	s := p.s
	var rec rawRecord
	if err := json.Unmarshal(line, &rec); err != nil {
		s.Malformed++
		return
	}
	if s.CWD == "" {
		s.CWD = rec.CWD
	}
	if s.Version == "" {
		s.Version = rec.Version
	}
	if s.Entrypoint == "" {
		s.Entrypoint = rec.Entrypoint
	}
	if ts := rec.Timestamp; ts != "" {
		if s.FirstTS == "" || tsBefore(ts, s.FirstTS) {
			s.FirstTS = ts
		}
		if s.LastTS == "" || tsBefore(s.LastTS, ts) {
			s.LastTS = ts
		}
	}
	switch rec.Type {
	case "system":
		if rec.Subtype == "compact_boundary" {
			s.Events = append(s.Events, Event{Kind: EvCompact})
			if !p.usageSeen {
				s.CompactBeforeUsage++
			}
		}
	case "fork-context-ref":
		s.Fork = true
	case "attachment":
		p.attachment(rec.Attachment)
	case "assistant":
		if rec.Message == nil {
			return
		}
		for _, b := range blocks(rec.Message.Content) {
			if b.Type == "tool_use" {
				_, dup := p.uses[b.ID]
				p.uses[b.ID] = b
				p.toolUse(b, rec.Timestamp, dup && b.ID != "")
			}
		}
		if u := rec.Message.Usage; u != nil {
			id := rec.Message.ID
			ev := Event{Kind: EvUsage, Usage: Usage{
				MessageID: id, Input: u.InputTokens,
				CacheRead: u.CacheReadInputTokens, CacheCreation: u.CacheCreationInputTokens,
			}}
			if ev.Usage.Context() > 0 {
				p.usageSeen = true
			}
			if i, seen := p.usageAt[id]; seen && id != "" {
				s.Events[i] = ev // last line of a split message wins, at the first line's position
				return
			}
			if id != "" {
				p.usageAt[id] = len(s.Events)
			}
			s.Events = append(s.Events, ev)
		}
	case "user":
		if rec.Message == nil {
			return
		}
		bs := blocks(rec.Message.Content)
		p.userText(rec.Message.Content, bs, rec.IsMeta)
		for _, b := range bs {
			if b.Type != "tool_result" {
				continue
			}
			tr := ToolResult{ToolUseID: b.ToolUseID, IsError: b.IsError}
			tr.Text, tr.NonText = resultText(b.Content)
			if use, ok := p.uses[b.ToolUseID]; ok {
				tr.ToolName, tr.ToolInput = use.Name, string(use.Input)
			} else {
				tr.ToolName = "?"
				s.Unmatched++
			}
			s.Events = append(s.Events, Event{Kind: EvToolResult, Result: tr})
		}
	}
}

// toolUse records one tool_use block; a block whose id was already seen (a repeated line) only
// moves LastTS.
func (p *parser) toolUse(b rawBlock, ts string, dup bool) {
	if b.Name == "" {
		return
	}
	c := p.calls[b.Name]
	if c == nil {
		if dup {
			return
		}
		c = &ToolCall{Name: b.Name}
		p.calls[b.Name] = c
	}
	if ts != "" && (c.LastTS == "" || tsBefore(c.LastTS, ts)) {
		c.LastTS = ts
	}
	if dup {
		return
	}
	c.Count++
	if b.Name == "Skill" {
		var in struct {
			Skill string `json:"skill"`
		}
		if json.Unmarshal(b.Input, &in) == nil && in.Skill != "" {
			p.skills[in.Skill] = true
		}
	}
}

// userText measures the text of a user record (a string, or its text blocks) and drops it: it
// collects <command-name> values over the whole file and, before the first usage, records isMeta
// text as user_meta and the first non-meta record without tool results as first_prompt.
func (p *parser) userText(content json.RawMessage, bs []rawBlock, isMeta bool) {
	n, hasResult := 0, false
	if str, ok := stringValue(content); ok {
		n = len(str)
		p.commandNames(str)
	}
	for _, b := range bs {
		switch b.Type {
		case "text":
			n += len(b.Text)
			p.commandNames(b.Text)
		case "tool_result":
			hasResult = true
		}
	}
	if p.usageSeen {
		return
	}
	switch {
	case isMeta:
		p.s.Pre = append(p.s.Pre, Attachment{Type: AttUserMeta, Bytes: n})
	case !hasResult && !p.firstPrompt:
		p.firstPrompt = true
		p.s.Pre = append(p.s.Pre, Attachment{Type: AttFirstPrompt, Bytes: n})
	}
}

// commandNames collects every <command-name>NAME</command-name> value, leading "/" stripped.
func (p *parser) commandNames(text string) {
	const open, closing = "<command-name>", "</command-name>"
	for {
		i := strings.Index(text, open)
		if i < 0 {
			return
		}
		text = text[i+len(open):]
		j := strings.Index(text, closing)
		if j < 0 {
			return
		}
		if name := strings.TrimPrefix(text[:j], "/"); name != "" {
			p.commands[name] = true
		}
		text = text[j+len(closing):]
	}
}

// stringValue decodes raw when it is a JSON string.
func stringValue(raw json.RawMessage) (string, bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || raw[0] != '"' {
		return "", false
	}
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return "", false
	}
	return s, true
}

// blocks decodes a content array; a string or anything else yields no blocks.
func blocks(raw json.RawMessage) []rawBlock {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || raw[0] != '[' {
		return nil
	}
	var bs []rawBlock
	if json.Unmarshal(raw, &bs) != nil {
		return nil
	}
	return bs
}

// resultText returns the text of a tool_result content (string or block array).
func resultText(raw json.RawMessage) (text string, nonText bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return "", false
	}
	if raw[0] == '"' {
		var s string
		if json.Unmarshal(raw, &s) == nil {
			return s, false
		}
		return "", false
	}
	var parts []string
	for _, b := range blocks(raw) {
		if b.Type == "text" {
			parts = append(parts, b.Text)
		} else {
			nonText = true
		}
	}
	return strings.Join(parts, "\n"), nonText
}
