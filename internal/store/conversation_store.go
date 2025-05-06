package store

import (
	"context"
	"errors"
	"skeleton-internship-backend/internal/dto"
	"sync"

	"github.com/google/uuid"
)

var (
	ErrConversationNotFound = errors.New("conversation not found")
)

type ConversationStore interface {
	CreateConversation(ctx context.Context) (string, error)
	GetHistory(ctx context.Context, conversationID string) ([]dto.ConversationTurn, error)
	AddTurn(ctx context.Context, conversationId string, turn dto.ConversationTurn) error
}

type inMemoryConversationStore struct {
	store map[string][]dto.ConversationTurn // map[conversationId][]Turns
	mu    sync.RWMutex
}

func NewInMemoryConversationStore() ConversationStore {
	return &inMemoryConversationStore{
		store: make(map[string][]dto.ConversationTurn),
	}
}
func (s *inMemoryConversationStore) CreateConversation(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	newID := uuid.NewString()
	s.store[newID] = make([]dto.ConversationTurn, 0)
	return newID, nil
}

func (s *inMemoryConversationStore) GetHistory(ctx context.Context, conversationId string) ([]dto.ConversationTurn, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if turns, ok := s.store[conversationId]; ok {
		return turns, nil
	}
	return nil, ErrConversationNotFound
}

func (s *inMemoryConversationStore) AddTurn(ctx context.Context, conversationId string, turn dto.ConversationTurn) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if turns, ok := s.store[conversationId]; ok {
		s.store[conversationId] = append(turns, turn)
		return nil
	}
	return ErrConversationNotFound
}
