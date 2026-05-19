package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// piRPCBackend is the long-lived bridge between the mdview browser chat and a
// background `pi --mode rpc` process. Keeping the process alive avoids paying
// startup cost for every browser message and lets Pi keep turn context for the
// lifetime of mdview.
type piRPCBackend struct {
	sessionDir string
	cmd        *exec.Cmd
	stdin      io.WriteCloser
	events     chan piJSONEvent
	done       chan error
	mu         sync.Mutex
	stderrMu   sync.Mutex
	stderr     bytes.Buffer
}

func newPiRPCBackend(ctx context.Context, path string) (*piRPCBackend, error) {
	bin := strings.TrimSpace(os.Getenv("MDVIEW_PI_BIN"))
	if bin == "" {
		var err error
		bin, err = exec.LookPath("pi")
		if err != nil {
			return nil, errors.New("pi not found in PATH")
		}
	}
	sessionDir, err := os.MkdirTemp("", "mdview-pi-")
	if err != nil {
		return nil, fmt.Errorf("create pi session dir: %w", err)
	}
	cmd := exec.CommandContext(ctx, bin, "--mode", "rpc", "--session-dir", sessionDir, "--append-system-prompt", mdviewSystemPrompt(path)) // #nosec G204,G702 -- pi binary is resolved from PATH/env by user request.
	cmd.Dir = filepath.Dir(path)
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		cmd.Dir = path
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		_ = os.RemoveAll(sessionDir)
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = os.RemoveAll(sessionDir)
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		_ = os.RemoveAll(sessionDir)
		return nil, err
	}
	backend := &piRPCBackend{
		sessionDir: sessionDir,
		cmd:        cmd,
		stdin:      stdin,
		events:     make(chan piJSONEvent, 64),
		done:       make(chan error, 1),
	}
	if err := cmd.Start(); err != nil {
		_ = os.RemoveAll(sessionDir)
		return nil, err
	}
	go backend.readStdout(stdout)
	go backend.readStderr(stderr)
	go func() {
		backend.done <- cmd.Wait()
	}()
	return backend, nil
}

func (b *piRPCBackend) readStdout(r io.Reader) {
	defer close(b.events)
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var event piJSONEvent
		if err := json.Unmarshal(line, &event); err != nil {
			continue
		}
		b.events <- event
	}
}

func (b *piRPCBackend) readStderr(r io.Reader) {
	buf := make([]byte, 4096)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			b.stderrMu.Lock()
			_, _ = b.stderr.Write(buf[:n])
			if b.stderr.Len() > 8192 {
				data := b.stderr.Bytes()
				b.stderr.Reset()
				_, _ = b.stderr.Write(data[len(data)-4096:])
			}
			b.stderrMu.Unlock()
		}
		if err != nil {
			return
		}
	}
}

func (b *piRPCBackend) Reply(ctx context.Context, path, message string, selection *chatSelection) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	id := fmt.Sprintf("mdview-%d", time.Now().UnixNano())
	payload, err := json.Marshal(map[string]any{
		"id":      id,
		"type":    "prompt",
		"message": mdviewPrompt(path, message, selection),
	})
	if err != nil {
		return "", err
	}
	payload = append(payload, '\n')
	if _, err := b.stdin.Write(payload); err != nil {
		return "", fmt.Errorf("send prompt to pi rpc: %w%s", err, b.stderrTail())
	}

	var deltas strings.Builder
	var lastAssistant string
	for {
		select {
		case <-ctx.Done():
			b.abortAndDrain()
			return "", ctx.Err()
		case err := <-b.done:
			if err != nil {
				return "", fmt.Errorf("pi rpc exited: %w%s", err, b.stderrTail())
			}
			return "", fmt.Errorf("pi rpc exited%s", b.stderrTail())
		case event, ok := <-b.events:
			if !ok {
				return "", fmt.Errorf("pi rpc closed%s", b.stderrTail())
			}
			if event.Type == "response" && event.ID == id && !event.Success {
				if event.Error != "" {
					return "", errors.New(event.Error)
				}
				return "", errors.New("pi rejected prompt")
			}
			if event.AssistantMessageEvent != nil && event.AssistantMessageEvent.Type == "text_delta" {
				deltas.WriteString(event.AssistantMessageEvent.Delta)
				continue
			}
			switch event.Type {
			case "message_end":
				if event.Message.Role == "assistant" {
					if text := piContentText(event.Message.Content); text != "" {
						lastAssistant = text
					}
				}
			case "agent_end":
				if text := strings.TrimSpace(deltas.String()); text != "" {
					return text, nil
				}
				if text := lastPiAssistantText(event.Messages); text != "" {
					return text, nil
				}
				if strings.TrimSpace(lastAssistant) != "" {
					return strings.TrimSpace(lastAssistant), nil
				}
				return "", errors.New("pi completed without assistant text")
			}
		}
	}
}

func (b *piRPCBackend) abortAndDrain() {
	_, _ = b.stdin.Write([]byte(`{"type":"abort"}` + "\n"))
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for {
		select {
		case <-timer.C:
			return
		case event, ok := <-b.events:
			if !ok || event.Type == "agent_end" {
				return
			}
		}
	}
}

func (b *piRPCBackend) stderrTail() string {
	b.stderrMu.Lock()
	defer b.stderrMu.Unlock()
	return commandStderr(b.stderr.String())
}

func (b *piRPCBackend) Close() error {
	if b.stdin != nil {
		_ = b.stdin.Close()
	}
	if b.cmd != nil && b.cmd.Process != nil {
		_ = b.cmd.Process.Kill()
	}
	if b.sessionDir != "" {
		_ = os.RemoveAll(b.sessionDir)
	}
	return nil
}

func piChatReply(ctx context.Context, path, message string, selection *chatSelection) (string, error) {
	bin := strings.TrimSpace(os.Getenv("MDVIEW_PI_BIN"))
	if bin == "" {
		var err error
		bin, err = exec.LookPath("pi")
		if err != nil {
			return "", errors.New("pi not found in PATH")
		}
	}
	prompt := mdviewPrompt(path, message, selection)
	cmd := exec.CommandContext(ctx, bin, "--mode", "json", "--append-system-prompt", mdviewSystemPrompt(path), prompt) // #nosec G204,G702 -- pi binary is resolved from PATH/env by user request.
	cmd.Dir = filepath.Dir(path)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("pi chat failed: %w%s", err, commandStderr(stderr.String()))
	}
	reply := strings.TrimSpace(assistantTextFromPiJSON(stdout.Bytes()))
	if reply == "" {
		return "", errors.New("pi chat returned an empty response")
	}
	return reply, nil
}

func mdviewSystemPrompt(path string) string {
	return fmt.Sprintf("You are assisting from an mdview browser chat sidebar for the Markdown document %s. The browser live-reloads this file. If the user asks for document changes, edit this Markdown file directly. Keep replies concise and focused on the document.", path)
}

func mdviewPrompt(path, message string, selection *chatSelection) string {
	parts := []string{"mdview document: " + path}
	if selection != nil && strings.TrimSpace(selection.Text) != "" {
		detail := "The user selected this rendered text"
		if selection.Anchor != "" {
			detail += " under heading: " + selection.Anchor
		}
		if selection.URL != "" {
			detail += " (" + selection.URL + ")"
		}
		parts = append(parts, detail+":\n\n~~~\n"+selection.Text+"\n~~~")
	}
	parts = append(parts, "User message:\n"+message)
	return strings.Join(parts, "\n\n")
}

type piJSONEvent struct {
	ID                    string                 `json:"id,omitempty"`
	Type                  string                 `json:"type"`
	Command               string                 `json:"command,omitempty"`
	Success               bool                   `json:"success,omitempty"`
	Error                 string                 `json:"error,omitempty"`
	AssistantMessageEvent *assistantMessageEvent `json:"assistantMessageEvent,omitempty"`
	Message               piAgentMessage         `json:"message,omitempty"`
	Messages              []piAgentMessage       `json:"messages,omitempty"`
}

type assistantMessageEvent struct {
	Type  string `json:"type"`
	Delta string `json:"delta,omitempty"`
}

type piAgentMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

func assistantTextFromPiJSON(data []byte) string {
	var deltas strings.Builder
	var lastAssistant string
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var event piJSONEvent
		if err := json.Unmarshal(line, &event); err != nil {
			continue
		}
		if event.AssistantMessageEvent != nil && event.AssistantMessageEvent.Type == "text_delta" {
			deltas.WriteString(event.AssistantMessageEvent.Delta)
			continue
		}
		switch event.Type {
		case "message_end":
			if event.Message.Role == "assistant" {
				if text := piContentText(event.Message.Content); text != "" {
					lastAssistant = text
				}
			}
		case "agent_end":
			if text := lastPiAssistantText(event.Messages); text != "" {
				lastAssistant = text
			}
		}
	}
	if text := strings.TrimSpace(deltas.String()); text != "" {
		return text
	}
	return strings.TrimSpace(lastAssistant)
}

func lastPiAssistantText(messages []piAgentMessage) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role != "assistant" {
			continue
		}
		if text := piContentText(messages[i].Content); text != "" {
			return text
		}
	}
	return ""
}

func piContentText(raw json.RawMessage) string {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return ""
	}
	var out strings.Builder
	for _, block := range blocks {
		if block.Type == "text" && block.Text != "" {
			out.WriteString(block.Text)
		}
	}
	return out.String()
}
