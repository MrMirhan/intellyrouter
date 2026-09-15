package notes

import (
	"sort"
	"sync"
)

// Note is a single note.
type Note struct {
	ID    int    `json:"id"`
	Title string `json:"title"`
	Body  string `json:"body"`
}

// Store keeps notes in memory. It is safe for concurrent use.
type Store struct {
	mu     sync.Mutex
	nextID int
	notes  map[int]Note
}

func NewStore() *Store {
	return &Store{nextID: 1, notes: make(map[int]Note)}
}

func (s *Store) Create(title, body string) Note {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := Note{ID: s.nextID, Title: title, Body: body}
	s.notes[n.ID] = n
	s.nextID++
	return n
}

func (s *Store) Get(id int) (Note, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n, ok := s.notes[id]
	return n, ok
}

func (s *Store) Delete(id int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.notes[id]; !ok {
		return false
	}
	delete(s.notes, id)
	return true
}

// List returns all notes ordered by ID.
func (s *Store) List() []Note {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Note
	for _, n := range s.notes {
		out = append(out, n)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
