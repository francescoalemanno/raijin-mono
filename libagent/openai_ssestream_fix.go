package libagent

import (
	"bufio"
	"bytes"
	"io"
	"strings"

	"github.com/charmbracelet/openai-go/packages/ssestream"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

func init() {
	// OpenCode Go emits legal SSE comment keepalives like ": OPENROUTER PROCESSING".
	// openai-go's default decoder turns the blank line after those comments into an
	// empty event, which later fails JSON unmarshalling. Replace the decoder with
	// one that skips comment-only / empty events.
	registerOpenAICompatibleSSEDecoder("text/event-stream")
	registerOpenAICompatibleSSEDecoder("text/event-stream; charset=utf-8")
}

func registerOpenAICompatibleSSEDecoder(contentType string) {
	ssestream.RegisterDecoder(contentType, func(rc io.ReadCloser) ssestream.Decoder {
		scn := bufio.NewScanner(rc)
		scn.Buffer(nil, bufio.MaxScanTokenSize<<9)
		return &commentTolerantEventStreamDecoder{
			rc:  rc,
			scn: scn,
		}
	})
}

type commentTolerantEventStreamDecoder struct {
	evt ssestream.Event
	rc  io.ReadCloser
	scn *bufio.Scanner
	err error
}

func (s *commentTolerantEventStreamDecoder) Next() bool {
	if s.err != nil {
		return false
	}

	event := ""
	data := bytes.NewBuffer(nil)

	for s.scn.Scan() {
		txt := s.scn.Bytes()

		if len(txt) == 0 {
			// A blank line after SSE comments is not a real event.
			if event == "" && data.Len() == 0 {
				continue
			}
			eventData := normalizeOpenAICompatibleReasoningFields(data.Bytes())
			s.evt = ssestream.Event{
				Type: event,
				Data: eventData,
			}
			return true
		}

		name, value, _ := bytes.Cut(txt, []byte(":"))
		if len(value) > 0 && value[0] == ' ' {
			value = value[1:]
		}

		switch string(name) {
		case "":
			continue
		case "event":
			event = string(value)
		case "data":
			_, s.err = data.Write(value)
			if s.err != nil {
				return false
			}
			_, s.err = data.WriteRune('\n')
			if s.err != nil {
				return false
			}
		}
	}

	if s.scn.Err() != nil {
		s.err = s.scn.Err()
	}

	return false
}

func (s *commentTolerantEventStreamDecoder) Event() ssestream.Event {
	return s.evt
}

func (s *commentTolerantEventStreamDecoder) Close() error {
	return s.rc.Close()
}

func (s *commentTolerantEventStreamDecoder) Err() error {
	return s.err
}

var _ ssestream.Decoder = (*commentTolerantEventStreamDecoder)(nil)

func normalizeOpenAICompatibleReasoningFields(data []byte) []byte {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || bytes.HasPrefix(trimmed, []byte("[DONE]")) || !gjson.ValidBytes(trimmed) {
		return data
	}

	normalized := trimmed
	changed := false
	paths := []string{"delta", "message"}
	reasoningFields := []string{"reasoning", "reasoning_text"}

	choices := gjson.GetBytes(trimmed, "choices")
	if !choices.IsArray() {
		return data
	}

	choices.ForEach(func(key, _ gjson.Result) bool {
		for _, basePath := range paths {
			contentPath := "choices." + key.String() + "." + basePath + ".reasoning_content"
			if content := gjson.GetBytes(normalized, contentPath); content.Exists() && strings.TrimSpace(content.String()) != "" {
				continue
			}
			for _, field := range reasoningFields {
				fieldPath := "choices." + key.String() + "." + basePath + "." + field
				value := gjson.GetBytes(normalized, fieldPath)
				if !value.Exists() || strings.TrimSpace(value.String()) == "" {
					continue
				}
				updated, err := sjson.SetBytes(normalized, contentPath, value.String())
				if err != nil {
					return true
				}
				normalized = updated
				changed = true
				break
			}
		}
		return true
	})

	if !changed {
		return data
	}
	return normalized
}
