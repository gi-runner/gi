package demoapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"uuid"
)

type User struct {
	ID    string   `json:"id"`
	Name  string   `json:"name"`
	Age   int      `json:"age"`
	Email string   `json:"email,omitempty"`
	Tags  []string `json:"tags"`
}

func Handler() http.Handler {
	s := &store{}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	})
	mux.HandleFunc("POST /users", s.create)
	mux.HandleFunc("GET /users", s.list)
	mux.HandleFunc("GET /users/{id}", s.user)
	return mux
}

type store struct {
	mu    sync.Mutex
	order []string
	users map[string]User
}

func (s *store) create(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name  string   `json:"name"`
		Age   int      `json:"age"`
		Email string   `json:"email"`
		Tags  []string `json:"tags"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json"})
		return
	}
	if strings.TrimSpace(in.Name) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "name is required"})
		return
	}
	if in.Tags == nil {
		in.Tags = []string{}
	}
	u := User{ID: uuid.New().String(), Name: in.Name, Age: in.Age, Email: in.Email, Tags: in.Tags}
	s.mu.Lock()
	if s.users == nil {
		s.users = map[string]User{}
	}
	s.users[u.ID] = u
	s.order = append(s.order, u.ID)
	s.mu.Unlock()
	writeJSON(w, http.StatusCreated, u)
}

func (s *store) list(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	out := make([]User, 0, len(s.order))
	for _, id := range s.order {
		out = append(out, s.users[id])
	}
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, out)
}

func (s *store) user(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "not found"})
		return
	}
	s.mu.Lock()
	u, ok := s.users[id]
	s.mu.Unlock()
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "not found"})
		return
	}
	writeJSON(w, http.StatusOK, u)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		return
	}
}
