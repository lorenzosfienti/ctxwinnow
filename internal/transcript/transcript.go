// Package transcript reads Claude Code session transcripts (~/.claude/projects/**/*.jsonl).
package transcript

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
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
	FirstTS, LastTS string // RFC 3339 timestamps as written in the file ("" if none)
	Events          []Event
	Malformed       int // non-blank lines that were not valid JSON
	Unmatched       int // tool_results without a matching tool_use
}

type rawRecord struct {
	Type      string `json:"type"`
	Subtype   string `json:"subtype"`
	CWD       string `json:"cwd"`
	Timestamp string `json:"timestamp"`
	Message   *struct {
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
	s       *Session
	uses    map[string]rawBlock
	usageAt map[string]int // message id -> index of its usage event
}

// Parse reads one transcript. Malformed lines are counted and skipped; only read errors are
// returned. Lines of any length are supported.
func Parse(r io.Reader) (*Session, error) {
	p := parser{s: &Session{}, uses: map[string]rawBlock{}, usageAt: map[string]int{}}
	br := bufio.NewReaderSize(r, 1<<20)
	for {
		line, err := br.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) > 0 {
			p.line(line)
		}
		if errors.Is(err, io.EOF) {
			return p.s, nil
		}
		if err != nil {
			return p.s, err
		}
	}
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
	if ts := rec.Timestamp; ts != "" {
		if s.FirstTS == "" || ts < s.FirstTS {
			s.FirstTS = ts
		}
		if ts > s.LastTS {
			s.LastTS = ts
		}
	}
	switch rec.Type {
	case "system":
		if rec.Subtype == "compact_boundary" {
			s.Events = append(s.Events, Event{Kind: EvCompact})
		}
	case "assistant":
		if rec.Message == nil {
			return
		}
		for _, b := range blocks(rec.Message.Content) {
			if b.Type == "tool_use" {
				p.uses[b.ID] = b
			}
		}
		if u := rec.Message.Usage; u != nil {
			id := rec.Message.ID
			ev := Event{Kind: EvUsage, Usage: Usage{
				MessageID: id, Input: u.InputTokens,
				CacheRead: u.CacheReadInputTokens, CacheCreation: u.CacheCreationInputTokens,
			}}
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
		for _, b := range blocks(rec.Message.Content) {
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
