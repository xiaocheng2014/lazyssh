package vault

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	// Production uses 2^18. A smaller factor keeps race-enabled tests fast while
	// exercising the same age/scrypt archive format and password flow.
	scryptWorkFactor = 10
	os.Exit(m.Run())
}

func TestVaultCreateSaveUnlock(t *testing.T) {
	root := t.TempDir()
	vaultDir := filepath.Join(root, "vault")
	sourceConfig := filepath.Join(root, "ssh-config")
	if err := os.WriteFile(sourceConfig, []byte("Host test\n    HostName 127.0.0.1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	password := []byte("correct horse battery staple")

	manager, err := Create(vaultDir, password, map[string]string{ConfigName: sourceConfig})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manager.Path(MetadataName), []byte(`{"test":{"tags":["dev"]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := manager.Close(); err != nil {
		t.Fatal(err)
	}

	unlocked, err := Unlock(vaultDir, password)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := unlocked.Close(); err != nil {
			t.Error(err)
		}
	}()
	config, err := os.ReadFile(unlocked.Path(ConfigName))
	if err != nil {
		t.Fatal(err)
	}
	if string(config) != "Host test\n    HostName 127.0.0.1\n" {
		t.Fatalf("unexpected config: %q", config)
	}
	metadata, err := os.ReadFile(unlocked.Path(MetadataName))
	if err != nil {
		t.Fatal(err)
	}
	if string(metadata) != `{"test":{"tags":["dev"]}}` {
		t.Fatalf("unexpected metadata: %q", metadata)
	}
}

func TestCloseReadOnlyDoesNotRewriteBundle(t *testing.T) {
	vaultDir := filepath.Join(t.TempDir(), "vault")
	password := []byte("read only password")
	manager, err := Create(vaultDir, password, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Close(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(BundlePath(vaultDir))
	if err != nil {
		t.Fatal(err)
	}

	unlocked, err := Unlock(vaultDir, password)
	if err != nil {
		t.Fatal(err)
	}
	if err := unlocked.CloseReadOnly(); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(BundlePath(vaultDir))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("read-only close rewrote the encrypted bundle")
	}
}

func TestVaultRejectsWrongPassword(t *testing.T) {
	vaultDir := filepath.Join(t.TempDir(), "vault")
	manager, err := Create(vaultDir, []byte("right password"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Unlock(vaultDir, []byte("wrong password")); err == nil {
		t.Fatal("Unlock() succeeded with the wrong password")
	}
}

func TestVaultAutomaticallyRemovesStaleLock(t *testing.T) {
	vaultDir := filepath.Join(t.TempDir(), "vault")
	password := []byte("right password")
	manager, err := Create(vaultDir, password, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Close(); err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(vaultDir, lockName)
	if err := os.WriteFile(lockPath, []byte("2147483647\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	unlocked, err := Unlock(vaultDir, password)
	if err != nil {
		t.Fatalf("unlock with stale lock: %v", err)
	}
	if err := unlocked.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestVaultRejectsLockOwnedByLiveProcess(t *testing.T) {
	vaultDir := filepath.Join(t.TempDir(), "vault")
	password := []byte("right password")
	manager, err := Create(vaultDir, password, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Close(); err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(vaultDir, lockName)
	if err := os.WriteFile(lockPath, []byte(strconv.Itoa(os.Getpid())+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(lockPath)

	if _, err := Unlock(vaultDir, password); err == nil {
		t.Fatal("unlock succeeded while lock owner is alive")
	} else if !strings.Contains(err.Error(), "already open by process") {
		t.Fatalf("unexpected lock error: %v", err)
	}
}

func TestVaultChangePassword(t *testing.T) {
	vaultDir := filepath.Join(t.TempDir(), "vault")
	oldPassword := []byte("old password value")
	newPassword := []byte("new password value")
	manager, err := Create(vaultDir, oldPassword, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.SaveLocalPassword(); err != nil {
		t.Fatal(err)
	}
	if err := manager.ChangePassword(newPassword); err != nil {
		t.Fatal(err)
	}
	localPassword, found, err := LoadLocalPassword(vaultDir)
	if err != nil {
		t.Fatal(err)
	}
	if !found || !bytes.Equal(localPassword, newPassword) {
		t.Fatalf("local password was not updated: found=%v value=%q", found, localPassword)
	}
	clear(localPassword)
	if err := manager.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Unlock(vaultDir, oldPassword); err == nil {
		t.Fatal("old password still unlocks the vault")
	}
	unlocked, err := Unlock(vaultDir, newPassword)
	if err != nil {
		t.Fatalf("new password does not unlock vault: %v", err)
	}
	if err := unlocked.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestLocalPasswordIsPreservedButExcludedFromBundle(t *testing.T) {
	vaultDir := filepath.Join(t.TempDir(), "vault")
	password := []byte("local password value")
	if err := os.MkdirAll(vaultDir, 0o700); err != nil {
		t.Fatal(err)
	}
	backupPath := filepath.Join(vaultDir, BundleName+".unreadable-backup")
	if err := os.WriteFile(backupPath, []byte("old encrypted bundle"), 0o600); err != nil {
		t.Fatal(err)
	}
	manager, err := Create(vaultDir, password, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.SaveLocalPassword(); err != nil {
		t.Fatal(err)
	}
	passwordPath := LocalPasswordPath(vaultDir)
	info, err := os.Stat(passwordPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("local password mode = %o", info.Mode().Perm())
	}
	if err := manager.SealPlaintext(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(passwordPath); err != nil {
		t.Fatalf("local password was removed while sealing: %v", err)
	}
	if _, err := os.Stat(backupPath); err != nil {
		t.Fatalf("encrypted bundle backup was removed while sealing: %v", err)
	}
	if err := manager.Close(); err != nil {
		t.Fatal(err)
	}

	unlocked, err := Unlock(vaultDir, password)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(unlocked.Path(LocalPasswordName)); !os.IsNotExist(err) {
		t.Fatalf("local password unexpectedly exists inside encrypted bundle: %v", err)
	}
	if _, err := os.Stat(unlocked.Path(filepath.Base(backupPath))); !os.IsNotExist(err) {
		t.Fatalf("encrypted bundle backup unexpectedly exists inside encrypted bundle: %v", err)
	}
	if err := unlocked.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestSecureArchivePath(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"../secret", "/absolute", "nested/../../secret"} {
		if _, err := secureArchivePath(root, name); err == nil {
			t.Fatalf("secureArchivePath(%q) succeeded", name)
		}
	}
	if _, err := secureArchivePath(root, "keys/id_ed25519"); err != nil {
		t.Fatalf("valid path rejected: %v", err)
	}
}
