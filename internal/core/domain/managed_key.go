package domain

import "time"

type ManagedKey struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	KeyType     string    `json:"key_type"`
	Fingerprint string    `json:"fingerprint"`
	PrivateFile string    `json:"private_file,omitempty"`
	PublicFile  string    `json:"public_file"`
	CreatedAt   time.Time `json:"created_at"`
}

func (k ManagedKey) HasPrivateKey() bool {
	return k.PrivateFile != ""
}
