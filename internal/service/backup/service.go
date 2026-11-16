package backup

import (
	"errors"
	"strings"

	"github.com/MardukPrime/Fast-V/internal/model"
	"github.com/MardukPrime/Fast-V/internal/store"
)

// Service exposes configuration export and restore to the frontend. The actual
// backup format is owned by the store package, which keeps settings,
// subscriptions, nodes, routing rules and the active node in one versioned JSON
// document.
type Service struct {
	store *store.Store
}

func New(st *store.Store) *Service {
	return &Service{store: st}
}

func (s *Service) ServiceName() string {
	return "BackupService"
}

// Export returns the complete FastV configuration as indented JSON. The frontend
// uses the string to download or copy a backup file.
func (s *Service) Export() (string, error) {
	data, err := s.store.Export()
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// Import replaces the current configuration with the provided backup. Invalid
// input is rejected with a descriptive error and the existing configuration is
// left untouched.
func (s *Service) Import(data string) (model.BackupResult, error) {
	if strings.TrimSpace(data) == "" {
		return model.BackupResult{}, errors.New("备份内容为空")
	}
	return s.store.Restore([]byte(data))
}
