package server

import (
	"encoding/json"
	"net/http"
	"sync"
	"sync/atomic"
)

type Todo struct {
	ID    int    `json:"id"`
	Title string `json:"title"`
	Done  bool   `json:"done"`
}

// Server holds the in-memory todo store. Handlers run concurrently, so
// access to todos is guarded by mu.
type Server struct {
	version  string
	notReady atomic.Bool

	mu    sync.RWMutex
	todos []Todo
}

func New(version string) *Server {
	return &Server{
		version: version,
		todos: []Todo{
			{ID: 1, Title: "Learn Kubernetes Pods", Done: true},
			{ID: 2, Title: "Deploy a Go API", Done: false},
			{ID: 3, Title: "Understand Services", Done: false},
		},
	}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()

	// Liveness probe: is the application process alive?
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})

	// Readiness probe: can the application receive traffic?
	// Fails once shutdown starts so the pod is removed from endpoints.
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if s.notReady.Load() {
			http.Error(w, "SHUTTING DOWN", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("READY"))
	})

	// Build version (git SHA), used to verify which code is live.
	mux.HandleFunc("GET /version", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte(s.version))
	})

	mux.HandleFunc("GET /todos", s.listTodos)
	mux.HandleFunc("POST /todos", s.createTodo)

	return mux
}

// SetNotReady makes /readyz fail. Called at the start of graceful shutdown.
func (s *Server) SetNotReady() {
	s.notReady.Store(true)
}

// Return all todos.
func (s *Server) listTodos(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(s.todos)
}

// Create a todo.
func (s *Server) createTodo(w http.ResponseWriter, r *http.Request) {
	var todo Todo

	if err := json.NewDecoder(r.Body).Decode(&todo); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	todo.ID = len(s.todos) + 1
	s.todos = append(s.todos, todo)
	s.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(todo)
}
