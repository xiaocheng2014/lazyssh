package vault

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	// Production uses 2^18. A smaller factor keeps race-enabled tests fast while
	// exercising the same age/scrypt archive format and password flow.
	scryptWorkFactor = 10
	os.Exit(m.Run())
}

func TestRecommendedScryptWorkFactor(t *testing.T) {
	if got := recommendedScryptWorkFactor("linux", "386"); got != ishScryptWorkFactor {
		t.Fatalf("linux/386 work factor = %d, want %d", got, ishScryptWorkFactor)
	}
	for _, platform := range []struct {
		goos   string
		goarch string
	}{
		{goos: "linux", goarch: "amd64"},
		{goos: "darwin", goarch: "arm64"},
		{goos: "windows", goarch: "386"},
	} {
		if got := recommendedScryptWorkFactor(platform.goos, platform.goarch); got != desktopScryptWorkFactor {
			t.Fatalf("%s/%s work factor = %d, want %d", platform.goos, platform.goarch, got, desktopScryptWorkFactor)
		}
	}
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

func TestVaultPreservesScryptWorkFactorFromManifest(t *testing.T) {
	const preservedWorkFactor = 11
	vaultDir := filepath.Join(t.TempDir(), "vault")
	password := []byte("portable vault password")
	manager, err := Create(vaultDir, password, nil)
	if err != nil {
		t.Fatal(err)
	}

	manifestData, err := os.ReadFile(manager.Path(ManifestName))
	if err != nil {
		t.Fatal(err)
	}
	var manifest Manifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.ScryptWorkFactor != scryptWorkFactor {
		t.Fatalf("created manifest work factor = %d, want %d", manifest.ScryptWorkFactor, scryptWorkFactor)
	}
	manifest.ScryptWorkFactor = preservedWorkFactor
	manifestData, err = json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manager.Path(ManifestName), manifestData, 0o600); err != nil {
		t.Fatal(err)
	}
	manager.scryptWorkFactor = preservedWorkFactor
	if err := manager.Close(); err != nil {
		t.Fatal(err)
	}

	unlocked, err := Unlock(vaultDir, password)
	if err != nil {
		t.Fatal(err)
	}
	if unlocked.scryptWorkFactor != preservedWorkFactor {
		t.Fatalf("unlocked work factor = %d, want %d", unlocked.scryptWorkFactor, preservedWorkFactor)
	}
	if err := unlocked.Close(); err != nil {
		t.Fatal(err)
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

func TestVaultWaitsForShortLivedWriteLock(t *testing.T) {
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
	if err := acquireLock(lockPath); err != nil {
		t.Fatal(err)
	}
	released := make(chan struct{})
	go func() {
		time.Sleep(50 * time.Millisecond)
		releaseLock(lockPath)
		close(released)
	}()

	unlocked, err := Unlock(vaultDir, password)
	if err != nil {
		t.Fatalf("unlock did not wait for short write lock: %v", err)
	}
	<-released
	if err := unlocked.CloseReadOnly(); err != nil {
		t.Fatal(err)
	}
}

func TestVaultAllowsConcurrentUnlock(t *testing.T) {
	vaultDir := filepath.Join(t.TempDir(), "vault")
	password := []byte("concurrent window password")
	first, err := Create(vaultDir, password, nil)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Unlock(vaultDir, password)
	if err != nil {
		_ = first.CloseReadOnly()
		t.Fatalf("second concurrent unlock failed: %v", err)
	}
	if first.WorkDir() == second.WorkDir() {
		t.Fatal("concurrent managers unexpectedly share a plaintext runtime directory")
	}
	if err := first.CloseReadOnly(); err != nil {
		t.Fatal(err)
	}
	if err := second.CloseReadOnly(); err != nil {
		t.Fatal(err)
	}
}

func TestVaultPreventsStaleWindowFromOverwritingNewerSave(t *testing.T) {
	vaultDir := filepath.Join(t.TempDir(), "vault")
	password := []byte("concurrent write password")
	first, err := Create(vaultDir, password, nil)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Unlock(vaultDir, password)
	if err != nil {
		_ = first.CloseReadOnly()
		t.Fatal(err)
	}
	defer first.CloseReadOnly()
	defer second.CloseReadOnly()

	if err := os.WriteFile(first.Path(ConfigName), []byte("Host first\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := first.Save(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second.Path(ConfigName), []byte("Host stale\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := second.Save(); !errors.Is(err, ErrVaultChanged) {
		t.Fatalf("stale save error = %v, want ErrVaultChanged", err)
	}

	latest, err := Unlock(vaultDir, password)
	if err != nil {
		t.Fatal(err)
	}
	defer latest.CloseReadOnly()
	config, err := os.ReadFile(latest.Path(ConfigName))
	if err != nil {
		t.Fatal(err)
	}
	if string(config) != "Host first\n" {
		t.Fatalf("newer content was overwritten: %q", config)
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

func TestVaultVerifyPassword(t *testing.T) {
	vaultDir := filepath.Join(t.TempDir(), "vault")
	password := []byte("password to verify")
	manager, err := Create(vaultDir, password, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := manager.CloseReadOnly(); err != nil {
			t.Error(err)
		}
	}()
	if err := manager.VerifyPassword(password); err != nil {
		t.Fatalf("correct password rejected: %v", err)
	}
	if err := manager.VerifyPassword([]byte("incorrect password")); err == nil {
		t.Fatal("incorrect password accepted")
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
