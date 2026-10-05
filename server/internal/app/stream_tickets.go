package app

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

type streamTicket struct {
	ownerID string
	expires time.Time
}

type streamTicketStore struct {
	mu      sync.Mutex
	tickets map[string]streamTicket
}

func newStreamTicketStore() *streamTicketStore {
	return &streamTicketStore{tickets: make(map[string]streamTicket)}
}

func (s *streamTicketStore) issue(ownerID string) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	token := hex.EncodeToString(b)
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for ticket, value := range s.tickets {
		if now.After(value.expires) {
			delete(s.tickets, ticket)
		}
	}
	s.tickets[token] = streamTicket{ownerID: ownerID, expires: now.Add(30 * time.Second)}
	return token, nil
}

func (s *streamTicketStore) consume(token, ownerID string) bool {
	if token == "" || ownerID == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ticket, ok := s.tickets[token]
	delete(s.tickets, token)
	return ok && ticket.ownerID == ownerID && time.Now().Before(ticket.expires)
}
