package server

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/owenrumney/mdview/internal/browser"
	"github.com/owenrumney/mdview/internal/render"
)

func Run(ctx context.Context, path string, opts render.Options) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolve path: %w", err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return fmt.Errorf("stat %s: %w", abs, err)
	}
	doc := servedDocument{root: abs, dir: info.IsDir()}
	if doc.dir {
		opts.Contents = true
		doc.files, err = markdownFiles(abs)
		if err != nil {
			return err
		}
		if len(doc.files) == 0 {
			return fmt.Errorf("directory %s contains no markdown files", abs)
		}
	}

	hub := newReloadHub()
	chatToken, err := newChatToken(opts.Chat)
	if err != nil {
		return err
	}
	var backend chatBackend
	if opts.Chat {
		switch opts.ChatAgent {
		case "pi":
			backend, err = newPiRPCBackend(ctx, abs)
		case "claude":
			backend, err = newClaudeRPCBackend(ctx, abs, opts.ChatSession)
		}
		if err != nil {
			return err
		}
		if backend != nil {
			defer func() { _ = backend.Close() }()
		}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		reqDoc, err := doc.withFreshFiles()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		current := ""
		if reqDoc.dir {
			if file := r.URL.Query().Get("file"); file != "" {
				if _, current, err = reqDoc.selectedFile(file); err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
			}
		} else {
			current = filepath.Base(reqDoc.root)
		}
		reqOpts := opts
		reqOpts.WatchMode = true
		reqOpts.ChatToken = chatToken
		reqOpts.CurrentFile = current
		reqOpts.DirFiles = reqDoc.fileTree(current)
		var body []byte
		if reqDoc.dir {
			body, err = render.Shell(filepath.Base(reqDoc.root), reqOpts)
		} else {
			body, err = render.File(reqDoc.root, reqOpts)
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(body)
	})
	mux.HandleFunc("/document", func(w http.ResponseWriter, r *http.Request) {
		if !doc.dir {
			http.NotFound(w, r)
			return
		}
		reqDoc, err := doc.withFreshFiles()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		selected, current, err := reqDoc.selectedFile(r.URL.Query().Get("file"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		reqOpts := opts
		reqOpts.CurrentFile = current
		rendered, err := render.Document(selected, reqOpts)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(documentResponse{File: current, Title: rendered.Title, Body: string(rendered.Body), TOC: rendered.TOC})
	})
	mux.HandleFunc("/assets/mermaid.min.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		_, _ = w.Write(render.MermaidJS)
	})
	mux.HandleFunc("/events", hub.handleSSE)
	if opts.Chat {
		mux.HandleFunc("/chat", handleChat(doc, chatToken, opts, backend))
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	url := "http://" + listener.Addr().String()

	srv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("fsnotify: %w", err)
	}
	defer func() { _ = watcher.Close() }()
	watchTarget := abs
	if doc.dir {
		if err := watchDirs(watcher, abs); err != nil {
			return err
		}
		watchTarget = ""
	} else {
		if err := watcher.Add(abs); err != nil {
			return fmt.Errorf("watch file: %w", err)
		}
		if err := watcher.Add(filepath.Dir(abs)); err != nil {
			return fmt.Errorf("watch dir: %w", err)
		}
	}

	go watchLoop(ctx, watcher, watchTarget, hub)

	serverErr := make(chan error, 1)
	go func() {
		err := srv.Serve(listener)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
			return
		}
		serverErr <- nil
	}()

	slog.Info("mdview live", "url", url, "file", abs)
	if err := browser.Open(url); err != nil {
		slog.Warn("could not open browser", "err", err)
	}

	select {
	case <-ctx.Done():
	case err := <-serverErr:
		if err != nil {
			return err
		}
	}

	slog.Info("shutting down")
	hub.close()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return nil
}

type servedDocument struct {
	root  string
	dir   bool
	files []string
}

func (d servedDocument) withFreshFiles() (servedDocument, error) {
	if !d.dir {
		return d, nil
	}
	files, err := markdownFiles(d.root)
	if err != nil {
		return d, err
	}
	d.files = files
	return d, nil
}

func (d servedDocument) selected(r *http.Request) (string, string, error) {
	if !d.dir {
		return d.root, filepath.Base(d.root), nil
	}
	return d.selectedFile(r.URL.Query().Get("file"))
}

func (d servedDocument) selectedFile(name string) (string, string, error) {
	if !d.dir {
		return d.root, filepath.Base(d.root), nil
	}
	name = cleanRelativeFile(name)
	if name == "" {
		return "", "", errors.New("file is required")
	}
	for _, file := range d.files {
		if file == name {
			return filepath.Join(d.root, filepath.FromSlash(name)), name, nil
		}
	}
	return "", "", fmt.Errorf("unknown markdown file %q", name)
}

func (d servedDocument) fileFromRequest(name string) (string, error) {
	path, _, err := d.selectedFile(name)
	return path, err
}

func (d servedDocument) fileTree(current string) []render.FileEntry {
	if !d.dir {
		return nil
	}
	return buildFileTree(d.files, current)
}

func cleanRelativeFile(name string) string {
	name = strings.TrimSpace(strings.ReplaceAll(name, "\\", "/"))
	if name == "" {
		return ""
	}
	name = pathClean(name)
	if name == "." || strings.HasPrefix(name, "../") || strings.HasPrefix(name, "/") {
		return ""
	}
	return name
}

func pathClean(value string) string {
	cleaned := filepath.ToSlash(filepath.Clean(value))
	return strings.TrimPrefix(cleaned, "./")
}

func buildFileTree(files []string, current string) []render.FileEntry {
	type node struct {
		entry    render.FileEntry
		children map[string]*node
	}
	root := &node{children: map[string]*node{}}
	for _, file := range files {
		parts := strings.Split(file, "/")
		parent := root
		for i, part := range parts {
			if part == "" {
				continue
			}
			isFile := i == len(parts)-1
			if isFile {
				parent.children[part] = &node{entry: render.FileEntry{Name: part, Path: file, Selected: file == current}}
				continue
			}
			child := parent.children[part]
			if child == nil {
				child = &node{entry: render.FileEntry{Name: part, Dir: true}, children: map[string]*node{}}
				parent.children[part] = child
			}
			parent = child
		}
	}
	var convert func(*node) []render.FileEntry
	convert = func(n *node) []render.FileEntry {
		keys := make([]string, 0, len(n.children))
		for key := range n.children {
			keys = append(keys, key)
		}
		slices.SortFunc(keys, func(a, b string) int {
			ad := n.children[a].entry.Dir
			bd := n.children[b].entry.Dir
			if ad != bd {
				if ad {
					return -1
				}
				return 1
			}
			return strings.Compare(strings.ToLower(a), strings.ToLower(b))
		})
		out := make([]render.FileEntry, 0, len(keys))
		for _, key := range keys {
			child := n.children[key]
			entry := child.entry
			if entry.Dir {
				entry.Children = convert(child)
			}
			out = append(out, entry)
		}
		return out
	}
	return convert(root)
}

func markdownFiles(dir string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if !isMarkdownPath(path) {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		files = append(files, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk dir %s: %w", dir, err)
	}
	slices.SortFunc(files, func(a, b string) int {
		return strings.Compare(strings.ToLower(a), strings.ToLower(b))
	})
	return files, nil
}

func newChatToken(enabled bool) (string, error) {
	if !enabled {
		return "", nil
	}
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate chat token: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

type documentResponse struct {
	File  string            `json:"file"`
	Title string            `json:"title"`
	Body  string            `json:"body"`
	TOC   []render.TOCEntry `json:"toc"`
}

type chatRequest struct {
	Message   string         `json:"message"`
	Selection *chatSelection `json:"selection,omitempty"`
	File      string         `json:"file,omitempty"`
}

type chatSelection struct {
	Text   string `json:"text"`
	Anchor string `json:"anchor,omitempty"`
	URL    string `json:"url,omitempty"`
}

type chatResponse struct {
	Reply     string `json:"reply"`
	ReplyHTML string `json:"replyHtml,omitempty"`
}

type chatBackend interface {
	Reply(ctx context.Context, path, message string, selection *chatSelection) (string, error)
	Close() error
}

func handleChat(doc servedDocument, token string, opts render.Options, backend chatBackend) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if token == "" || r.Header.Get("X-Mdview-Token") != token {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, 256*1024)
		defer func() { _ = r.Body.Close() }()
		var req chatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		if req.Message == "" {
			http.Error(w, "message is required", http.StatusBadRequest)
			return
		}

		reqDoc, err := doc.withFreshFiles()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		path, err := reqDoc.fileFromRequest(req.File)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		reply, err := chatReply(r.Context(), path, req.Message, req.Selection, opts, backend)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}

		replyHTML := ""
		chatRenderOpts := render.Options{Theme: opts.Theme}
		if html, err := render.Markdown([]byte(reply), chatRenderOpts); err == nil {
			replyHTML = string(html)
		}

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(chatResponse{Reply: reply, ReplyHTML: replyHTML})
	}
}

func chatReply(ctx context.Context, path, message string, selection *chatSelection, opts render.Options, backend chatBackend) (string, error) {
	if opts.ChatAgent == "pi" {
		if backend != nil {
			return backend.Reply(ctx, path, message, selection)
		}
		return piChatReply(ctx, path, message, selection)
	}
	if opts.ChatAgent == "claude" {
		if backend == nil {
			return "", errors.New("claude chat backend is not running")
		}
		return backend.Reply(ctx, path, message, selection)
	}
	if opts.ChatAgent == "watchtower" {
		if opts.ChatSession == "" {
			return "", errors.New("watchtower chat requires --chat-session")
		}
		bin := strings.TrimSpace(os.Getenv("MDVIEW_WATCHTOWER_BIN"))
		if bin == "" {
			var err error
			bin, err = exec.LookPath("watchtower")
			if err != nil {
				return "", errors.New("watchtower not found in PATH")
			}
		}
		args := []string{"chat", "prompt", "--session", opts.ChatSession, "--file", path}
		if selection != nil && strings.TrimSpace(selection.Text) != "" {
			args = append(args, "--selection", selection.Text)
			if selection.Anchor != "" {
				args = append(args, "--selection-anchor", selection.Anchor)
			}
			if selection.URL != "" {
				args = append(args, "--selection-url", selection.URL)
			}
		}
		cmd := exec.CommandContext(ctx, bin, args...) // #nosec G204,G702 -- watchtower binary is resolved from PATH/env by user request.
		cmd.Stdin = strings.NewReader(message)
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			return "", fmt.Errorf("watchtower chat failed: %w%s", err, commandStderr(stderr.String()))
		}
		reply := strings.TrimSpace(stdout.String())
		if reply == "" {
			return "", errors.New("watchtower chat returned an empty response")
		}
		return reply, nil
	}

	src, err := os.ReadFile(path) // #nosec G304 -- path is the CLI-selected markdown file
	if err != nil {
		return "", err
	}
	backendName := "mock"
	if opts.ChatAgent != "" {
		backendName = opts.ChatAgent
	}
	session := ""
	if opts.ChatSession != "" {
		session = fmt.Sprintf(" for session %s", opts.ChatSession)
	}
	return fmt.Sprintf("Chat UI is wired up to the %s backend%s, but no agent bridge is connected yet. I can see %s (%d bytes). You said: %s", backendName, session, filepath.Base(path), len(src), message), nil
}

func commandStderr(stderr string) string {
	stderr = strings.TrimSpace(stderr)
	if stderr == "" {
		return ""
	}
	if len(stderr) > 4000 {
		stderr = stderr[len(stderr)-4000:]
	}
	return "\nstderr:\n" + stderr
}

func isMarkdownPath(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".md" || ext == ".markdown"
}

func watchDirs(w *fsnotify.Watcher, root string) error {
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			return nil
		}
		if err := w.Add(path); err != nil {
			return fmt.Errorf("watch dir %s: %w", path, err)
		}
		return nil
	})
}

func watchLoop(ctx context.Context, w *fsnotify.Watcher, target string, hub *reloadHub) {
	var (
		mu      sync.Mutex
		pending bool
	)
	notify := func() {
		mu.Lock()
		if pending {
			mu.Unlock()
			return
		}
		pending = true
		mu.Unlock()
		time.AfterFunc(80*time.Millisecond, func() {
			mu.Lock()
			pending = false
			mu.Unlock()
			n := hub.broadcast()
			slog.Info("reload", "file", target, "clients", n)
		})
	}
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-w.Events:
			if !ok {
				return
			}
			if target != "" {
				if filepath.Clean(ev.Name) != target {
					continue
				}
				if ev.Has(fsnotify.Remove) || ev.Has(fsnotify.Rename) {
					time.AfterFunc(50*time.Millisecond, func() { _ = w.Add(target) })
				}
			} else {
				if ev.Has(fsnotify.Create) {
					if info, err := os.Stat(ev.Name); err == nil && info.IsDir() {
						_ = watchDirs(w, ev.Name)
						notify()
						continue
					}
				}
				if !isMarkdownPath(ev.Name) {
					continue
				}
			}
			notify()
		case err, ok := <-w.Errors:
			if !ok {
				return
			}
			slog.Warn("watcher error", "err", err)
		}
	}
}

type reloadHub struct {
	mu      sync.Mutex
	clients map[chan struct{}]struct{}
	done    chan struct{}
}

func newReloadHub() *reloadHub {
	return &reloadHub{
		clients: make(map[chan struct{}]struct{}),
		done:    make(chan struct{}),
	}
}

func (h *reloadHub) close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	select {
	case <-h.done:
		return
	default:
		close(h.done)
	}
}

func (h *reloadHub) subscribe() chan struct{} {
	ch := make(chan struct{}, 1)
	h.mu.Lock()
	h.clients[ch] = struct{}{}
	h.mu.Unlock()
	return ch
}

func (h *reloadHub) unsubscribe(ch chan struct{}) {
	h.mu.Lock()
	delete(h.clients, ch)
	h.mu.Unlock()
	close(ch)
}

func (h *reloadHub) broadcast() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.clients {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
	return len(h.clients)
}

func (h *reloadHub) handleSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch := h.subscribe()
	defer h.unsubscribe(ch)

	_, _ = fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()

	keepalive := time.NewTicker(20 * time.Second)
	defer keepalive.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-h.done:
			return
		case <-ch:
			_, _ = fmt.Fprint(w, "event: reload\ndata: 1\n\n")
			flusher.Flush()
		case <-keepalive.C:
			_, _ = fmt.Fprint(w, ": keepalive\n\n")
			flusher.Flush()
		}
	}
}
