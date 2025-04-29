package filestate

import (
	"encoding/json"
	"os"
	"sync"

	"github.com/rs/zerolog/log"
)

type FileProcessState map[string]int64
type Manager interface {
	LoadState() (FileProcessState, error)
	SaveState(state FileProcessState) error
	GetStateFilePath() string
}

type fileStateManager struct {
	filePath string
	mu       sync.RWMutex
}

func NewManager(filePath string) Manager {
	return &fileStateManager{
		filePath: filePath,
	}
}

func (m *fileStateManager) LoadState() (FileProcessState, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	data, err := os.ReadFile(m.filePath)
	if err != nil {
		log.Error().Err(err).Str("file", m.filePath).Msg("Raw error from os.ReadFile")
		if os.IsNotExist(err) {
			log.Warn().Str("file", m.filePath).Msg("State file confirmed not found, starting fresh.")
			return make(FileProcessState), nil
		}
		log.Error().Err(err).Str("file", m.filePath).Msg("Failed to read state file (not IsNotExist)")
		return nil, err
	}

	if len(data) == 0 {
		log.Warn().Str("file", m.filePath).Msg("State file is empty, starting fresh.")
		return make(FileProcessState), nil
	}
	var state FileProcessState
	if err := json.Unmarshal(data, &state); err != nil {
		log.Error().Err(err).Str("file", m.filePath).Msg("Failed to unmarshal state file")
		return nil, err
	}

	log.Debug().Str("file", m.filePath).Int("files_tracked", len(state)).Msg("Loaded file state")
	return state, nil

}
func (m *fileStateManager) SaveState(state FileProcessState) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		log.Error().Err(err).Msg("Failed to marshal state")
		return err
	}

	tempFilePath := m.filePath + ".tmp"
	err = os.WriteFile(tempFilePath, data, 0644)
	if err != nil {
		log.Error().Err(err).Str("file", tempFilePath).Msg("Failed to write temporary state file")
		return err
	}

	err = os.Rename(tempFilePath, m.filePath)
	if err != nil {
		log.Error().Err(err).Str("from", tempFilePath).Str("to", m.filePath).Msg("Failed to rename state file")
		// Attempt cleanup
		_ = os.Remove(tempFilePath)
		return err
	}
	log.Debug().Str("file", m.filePath).Int("files_tracked", len(state)).Msg("Saved file state")
	return nil
}

func (m *fileStateManager) GetStateFilePath() string {
	return m.filePath
}
