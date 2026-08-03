package ports

import "github.com/xiaocheng2014/lazyssh/internal/core/domain"

type KeyService interface {
	ListKeys() ([]domain.ManagedKey, error)
	ImportPrivateKey(name, sourcePath string) (domain.ManagedKey, error)
	ImportPublicKey(name, sourcePath string) (domain.ManagedKey, error)
	DeleteKey(id string) error
	PrivateKeyPath(id string) (string, error)
	PublicKey(id string) (string, error)
	ExportKey(id, destination string) error
}
