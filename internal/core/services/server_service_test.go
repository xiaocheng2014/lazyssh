package services

import (
	"bytes"
	"os"
	"reflect"
	"strings"
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

func TestValidateServerAcceptsChineseAlias(t *testing.T) {
	server := domain.Server{Alias: "生产数据库", Host: "192.0.2.10", Port: 22}
	if err := validateServer(server); err != nil {
		t.Fatalf("validateServer() rejected a Chinese alias: %v", err)
	}
}

func TestConnectionArgsUseInternalAliasForChineseDisplayAlias(t *testing.T) {
	service := &serverService{
		serverRepository: &serverServiceTestRepo{servers: []domain.Server{{Alias: "生产数据库"}}},
		sshConfigPath:    "/runtime/config",
	}
	args, err := service.connectionArgs("生产数据库")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := args[len(args)-1], domain.SSHConnectionAlias("生产数据库"); got != want {
		t.Fatalf("SSH destination = %q, want %q", got, want)
	}
	if strings.Contains(strings.Join(args, " "), "生产数据库") {
		t.Fatalf("Chinese alias leaked into OpenSSH command arguments: %v", args)
	}
}

func TestSavedPasswordUsesAskpassWithoutExposingSecret(t *testing.T) {
	const password = "server password #42"
	service := &serverService{
		serverRepository: &serverServiceTestRepo{servers: []domain.Server{{
			Alias:         "example",
			LoginPassword: password,
		}}},
		sshConfigPath: "/runtime/config",
	}
	args, savedPassword, err := service.connectionSpec("example")
	if err != nil {
		t.Fatal(err)
	}
	if savedPassword != password {
		t.Fatal("connectionSpec() did not return the saved password")
	}
	joinedArgs := strings.Join(args, " ")
	if !strings.Contains(joinedArgs, "BatchMode=no") || !strings.Contains(joinedArgs, "PasswordAuthentication=yes") {
		t.Fatalf("password authentication overrides are missing: %v", args)
	}

	command, cleanup, err := newSSHCommand(args, savedPassword)
	if err != nil {
		t.Fatal(err)
	}
	passwordPath := environmentValue(command.Env, askpassPasswordFileEnv)
	if passwordPath == "" {
		cleanup()
		t.Fatal("askpass password file environment is missing")
	}
	defer cleanup()
	if strings.Contains(strings.Join(command.Args, "\x00"), password) || strings.Contains(strings.Join(command.Env, "\x00"), password) {
		t.Fatal("saved password was exposed in process arguments or environment")
	}
	data, err := os.ReadFile(passwordPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != password {
		t.Fatalf("temporary password = %q", data)
	}

	t.Setenv(askpassPasswordFileEnv, passwordPath)
	var output bytes.Buffer
	handled, err := HandleSSHAskpass([]string{"user@example's password:"}, &output)
	if err != nil || !handled {
		t.Fatalf("HandleSSHAskpass() handled=%v err=%v", handled, err)
	}
	if output.String() != password+"\n" {
		t.Fatalf("askpass output = %q", output.String())
	}

	cleanup()
	if _, err := os.Stat(passwordPath); !os.IsNotExist(err) {
		t.Fatalf("temporary password file still exists: %v", err)
	}
}

func TestAskpassRejectsNonPasswordPrompt(t *testing.T) {
	passwordFile, err := os.CreateTemp("", "lazyssh-askpass-test-*")
	if err != nil {
		t.Fatal(err)
	}
	passwordPath := passwordFile.Name()
	if _, err := passwordFile.WriteString("must-not-be-returned"); err != nil {
		t.Fatal(err)
	}
	if err := passwordFile.Close(); err != nil {
		t.Fatal(err)
	}
	defer secureRemovePasswordFile(passwordPath)
	t.Setenv(askpassPasswordFileEnv, passwordPath)
	var output bytes.Buffer
	handled, err := HandleSSHAskpass([]string{"Continue connecting?"}, &output)
	if !handled || err == nil {
		t.Fatalf("HandleSSHAskpass() handled=%v err=%v", handled, err)
	}
	if output.Len() != 0 {
		t.Fatalf("non-password prompt received secret output: %q", output.String())
	}
}

func environmentValue(environment []string, key string) string {
	prefix := key + "="
	for _, item := range environment {
		if strings.HasPrefix(item, prefix) {
			return strings.TrimPrefix(item, prefix)
		}
	}
	return ""
}
