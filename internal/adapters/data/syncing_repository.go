package data

import (
	"fmt"

	"github.com/xiaocheng2014/lazyssh/internal/core/domain"
	"github.com/xiaocheng2014/lazyssh/internal/core/ports"
)

type syncingRepository struct {
	delegate ports.ServerRepository
	sync     func() error
}

// NewSyncingRepository persists the encrypted vault after every repository mutation.
func NewSyncingRepository(delegate ports.ServerRepository, sync func() error) ports.ServerRepository {
	return &syncingRepository{delegate: delegate, sync: sync}
}

func (r *syncingRepository) ListServers(query string) ([]domain.Server, error) {
	return r.delegate.ListServers(query)
}

func (r *syncingRepository) UpdateServer(server, newServer domain.Server) error {
	if err := r.delegate.UpdateServer(server, newServer); err != nil {
		return err
	}
	return r.syncVault()
}

func (r *syncingRepository) AddServer(server domain.Server) error {
	if err := r.delegate.AddServer(server); err != nil {
		return err
	}
	return r.syncVault()
}

func (r *syncingRepository) DeleteServer(server domain.Server) error {
	if err := r.delegate.DeleteServer(server); err != nil {
		return err
	}
	return r.syncVault()
}

func (r *syncingRepository) SetPinned(alias string, pinned bool) error {
	if err := r.delegate.SetPinned(alias, pinned); err != nil {
		return err
	}
	return r.syncVault()
}

func (r *syncingRepository) SetManagedKey(alias, keyID string) error {
	if err := r.delegate.SetManagedKey(alias, keyID); err != nil {
		return err
	}
	return r.syncVault()
}

func (r *syncingRepository) RecordSSH(alias string) error {
	if err := r.delegate.RecordSSH(alias); err != nil {
		return err
	}
	return r.syncVault()
}

func (r *syncingRepository) syncVault() error {
	if r.sync == nil {
		return nil
	}
	if err := r.sync(); err != nil {
		return fmt.Errorf("save encrypted vault: %w", err)
	}
	return nil
}
