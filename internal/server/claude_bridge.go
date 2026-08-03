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

// claudeRPCBackend bridges the mdview browser chat to a long-lived
// `claude -p --input-format stream-json` process. Keeping the process alive
// preserves Claude Code turn context across browser messages and avoids paying
// startup cost per message, mirroring piRPCBackend.
//
// The subprocess inherits the user's interactive Claude Code login, so replies
// bill their Pro/Max subscription. To keep that true we strip ANTHROPIC_API_KEY
// and ANTHROPIC_AUTH_TOKEN from its environment: either present would silently
// switch Claude Code to API billing.
type claudeRPCBackend struct {
	cmd      *exec.Cmd
	stdin    io.WriteCloser
	events   chan claudeJSONEvent
	done     chan error
	mu       sync.Mutex
	stderrMu sync.Mutex
	stderr   bytes.Buffer
}

func newClaudeRPCBackend(ctx context.Context, path, session string) (*claudeRPCBackend, error) {
	bin := strings.TrimSpace(os.Getenv("MDVIEW_CLAUDE_BIN"))
	if bin == "" {
		var err error
		bin, err = exec.LookPath("claude")
		if err != nil {
			return nil, errors.New("claude not found in PATH")
		}
	}
	args := []string{
		"-p",
		"--input-format", "stream-json",
		"--output-format", "stream-json",
		"--verbose", // required by Claude Code when --print uses stream-json output
		"--append-system-prompt", mdviewSystemPrompt(path),
	}
	if session != "" {
		args = append(args, "--resume", session)
	}
	cmd := exec.CommandContext(ctx, bin, args...) // #nosec G204,G702 -- claude binary is resolved from PATH/env by user request.
	cmd.Env = withoutAnthropicKeys(os.Environ())
	cmd.Dir = filepath.Dir(path)
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		cmd.Dir = path
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	backend := &claudeRPCBackend{
		cmd:    cmd,
		stdin:  stdin,
		events: make(chan claudeJSONEvent, 64),
		done:   make(chan error, 1),
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	go backend.readStdout(stdout)
	go backend.readStderr(stderr)
	go func() {
		backend.done <- cmd.Wait()
	}()
	return backend, nil
}

func (b *claudeRPCBackend) readStdout(r io.Reader) {
	defer close(b.events)
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var event claudeJSONEvent
		if err := json.Unmarshal(line, &event); err != nil {
			continue
		}
		b.events <- event
	}
}

func (b *claudeRPCBackend) readStderr(r io.Reader) {
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

func (b *claudeRPCBackend) Reply(ctx context.Context, path, message string, selection *chatSelection) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	payload, err := json.Marshal(map[string]any{
		"type": "user",
		"message": map[string]any{
			"role":    "user",
			"content": mdviewPrompt(path, message, selection),
		},
	})
	if err != nil {
		return "", err
	}
	payload = append(payload, '\n')
	if _, err := b.stdin.Write(payload); err != nil {
		return "", fmt.Errorf("send prompt to claude: %w%s", err, b.stderrTail())
	}

	// Turns are serialized by b.mu, so the next "result" event is this turn's.
	for {
		select {
		case <-ctx.Done():
			b.abortAndDrain()
			return "", ctx.Err()
		case err := <-b.done:
			if err != nil {
				return "", fmt.Errorf("claude exited: %w%s", err, b.stderrTail())
			}
			return "", fmt.Errorf("claude exited%s", b.stderrTail())
		case event, ok := <-b.events:
			if !ok {
				return "", fmt.Errorf("claude closed%s", b.stderrTail())
			}
			if event.Type != "result" {
				continue
			}
			if event.IsError {
				if msg := strings.TrimSpace(event.Result); msg != "" {
					return "", errors.New(msg)
				}
				if event.Subtype != "" {
					return "", fmt.Errorf("claude turn failed: %s", event.Subtype)
				}
				return "", errors.New("claude turn failed")
			}
			if text := strings.TrimSpace(event.Result); text != "" {
				return text, nil
			}
			return "", errors.New("claude completed without assistant text")
		}
	}
}

func (b *claudeRPCBackend) abortAndDrain() {
	_, _ = b.stdin.Write([]byte(`{"type":"control_request","request_id":"mdview-abort","request":{"subtype":"interrupt"}}` + "\n"))
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for {
		select {
		case <-timer.C:
			return
		case event, ok := <-b.events:
			if !ok || event.Type == "result" {
				return
			}
		}
	}
}

func (b *claudeRPCBackend) stderrTail() string {
	b.stderrMu.Lock()
	defer b.stderrMu.Unlock()
	return commandStderr(b.stderr.String())
}

func (b *claudeRPCBackend) Close() error {
	if b.stdin != nil {
		_ = b.stdin.Close()
	}
	if b.cmd != nil && b.cmd.Process != nil {
		_ = b.cmd.Process.Kill()
	}
	return nil
}

// withoutAnthropicKeys returns env with any ANTHROPIC_API_KEY / ANTHROPIC_AUTH_TOKEN
// entries removed so the spawned claude uses the user's subscription login.
func withoutAnthropicKeys(env []string) []string {
	out := env[:0:0]
	for _, kv := range env {
		key, _, _ := strings.Cut(kv, "=")
		switch key {
		case "ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN":
			continue
		}
		out = append(out, kv)
	}
	return out
}

type claudeJSONEvent struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype,omitempty"`
	IsError bool   `json:"is_error,omitempty"`
	Result  string `json:"result,omitempty"`
}
