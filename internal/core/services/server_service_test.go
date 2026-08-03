package services

import (
	"reflect"
	"testing"

	"github.com/xiaocheng2014/lazyssh/internal/core/domain"
)

type serverServiceTestRepo struct {
	servers []domain.Server
}

func (r *serverServiceTestRepo) ListServers(string) ([]domain.Server, error)     { return r.servers, nil }
func (r *serverServiceTestRepo) UpdateServer(domain.Server, domain.Server) error { return nil }
func (r *serverServiceTestRepo) AddServer(domain.Server) error                   { return nil }
func (r *serverServiceTestRepo) DeleteServer(domain.Server) error                { return nil }
func (r *serverServiceTestRepo) SetPinned(string, bool) error                    { return nil }
func (r *serverServiceTestRepo) SetManagedKey(string, string) error              { return nil }
func (r *serverServiceTestRepo) RecordSSH(string) error                          { return nil }

type serverServiceTestKeys struct {
	path string
}

func (k *serverServiceTestKeys) ListKeys() ([]domain.ManagedKey, error) { return nil, nil }
func (k *serverServiceTestKeys) ImportPrivateKey(string, string) (domain.ManagedKey, error) {
	return domain.ManagedKey{}, nil
}
func (k *serverServiceTestKeys) ImportPublicKey(string, string) (domain.ManagedKey, error) {
	return domain.ManagedKey{}, nil
}
func (k *serverServiceTestKeys) DeleteKey(string) error                { return nil }
func (k *serverServiceTestKeys) PrivateKeyPath(string) (string, error) { return k.path, nil }
func (k *serverServiceTestKeys) PublicKey(string) (string, error)      { return "", nil }
func (k *serverServiceTestKeys) ExportKey(string, string) error        { return nil }

func TestSSHArgsUsesDedicatedConfig(t *testing.T) {
	service := &serverService{sshConfigPath: "/home/test/.config/lazyssh/config"}

	got := service.sshArgs("-G", "example")
	want := []string{"-F", "/home/test/.config/lazyssh/config", "-G", "example"}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sshArgs() = %v, want %v", got, want)
	}
}

func TestSSHArgsWithoutConfig(t *testing.T) {
	service := &serverService{}

	got := service.sshArgs("example")
	want := []string{"example"}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sshArgs() = %v, want %v", got, want)
	}
}

func TestConnectionArgsUsesManagedPrivateKey(t *testing.T) {
	service := &serverService{
		serverRepository: &serverServiceTestRepo{servers: []domain.Server{{Alias: "example", ManagedKeyID: "key-1"}}},
		keyService:       &serverServiceTestKeys{path: "/runtime/keys/key-1"},
		sshConfigPath:    "/runtime/config",
	}

	got, err := service.connectionArgs("example", "-N")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"-F", "/runtime/config", "-o", "IdentityFile=none", "-o", "IdentitiesOnly=yes", "-i", "/runtime/keys/key-1", "-N", "example"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("connectionArgs() = %v, want %v", got, want)
	}
}
