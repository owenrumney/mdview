package server

import (
	"bytes"
	"strings"
	"testing"
)

type bufferWriteCloser struct {
	bytes.Buffer
}

func (b *bufferWriteCloser) Close() error { return nil }

func TestAssistantTextFromPiJSONUsesStreamingDeltas(t *testing.T) {
	data := []byte(strings.Join([]string{
		`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"Hello"}}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":" world"}}`,
		`{"type":"agent_end","messages":[]}`,
	}, "\n"))

	got := assistantTextFromPiJSON(data)
	if got != "Hello world" {
		t.Fatalf("assistantTextFromPiJSON() = %q, want %q", got, "Hello world")
	}
}

func TestAssistantTextFromPiJSONFallsBackToAgentEndMessage(t *testing.T) {
	data := []byte(`{"type":"agent_end","messages":[{"role":"user","content":"question"},{"role":"assistant","content":[{"type":"text","text":"answer"}]}]}`)

	got := assistantTextFromPiJSON(data)
	if got != "answer" {
		t.Fatalf("assistantTextFromPiJSON() = %q, want %q", got, "answer")
	}
}

func TestPiRPCBackendAbortAndDrain(t *testing.T) {
	stdin := &bufferWriteCloser{}
	backend := &piRPCBackend{
		stdin:  stdin,
		events: make(chan piJSONEvent, 1),
		done:   make(chan error, 1),
	}
	backend.events <- piJSONEvent{Type: "agent_end"}

	backend.abortAndDrain()

	if got := stdin.String(); got != "{\"type\":\"abort\"}\n" {
		t.Fatalf("abort command = %q, want abort JSONL", got)
	}
}

func TestMdviewPromptIncludesSelectionContext(t *testing.T) {
	prompt := mdviewPrompt("/tmp/notes.md", "tighten this", &chatSelection{
		Text:   "selected text",
		Anchor: "Intro",
		URL:    "/#intro",
	})

	for _, want := range []string{"mdview document: /tmp/notes.md", "under heading: Intro", "selected text", "User message:\ntighten this"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt = %q, want to contain %q", prompt, want)
		}
	}
}
