package services

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestInspectPublicKey(t *testing.T) {
	line := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIKwJFQfW0JYoFaQhczqgVwVAs5UD25S6kWq8f6Y0M7ZL test"
	keyType, fingerprint, id, err := inspectPublicKey(line)
	if err != nil {
		t.Fatal(err)
	}
	if keyType != "ssh-ed25519" {
		t.Fatalf("key type = %q", keyType)
	}
	if fingerprint == "" || id == "" {
		t.Fatalf("missing fingerprint or id: %q %q", fingerprint, id)
	}
}

func TestInspectPublicKeyRejectsInvalidData(t *testing.T) {
	if _, _, _, err := inspectPublicKey("not-a-key"); err == nil {
		t.Fatal("invalid public key accepted")
	}
}

func TestImportAndExportPublicKey(t *testing.T) {
	root := t.TempDir()
	keysDir := filepath.Join(root, "keys")
	manifestPath := filepath.Join(root, "keys.json")
	source := filepath.Join(root, "source.pub")
	line := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIKwJFQfW0JYoFaQhczqgVwVAs5UD25S6kWq8f6Y0M7ZL test"
	if err := os.WriteFile(source, []byte(line+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	service := &keyService{keysDir: keysDir, manifestPath: manifestPath}
	key, err := service.ImportPublicKey("test key", source)
	if err != nil {
		t.Fatal(err)
	}
	if key.HasPrivateKey() {
		t.Fatal("public-only import unexpectedly has a private key")
	}
	destination := filepath.Join(root, "restored", "test.pub")
	if err := service.ExportKey(key.ID, destination); err != nil {
		t.Fatal(err)
	}
	restored, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(restored)) != line {
		t.Fatalf("restored public key = %q", restored)
	}
}

func TestImportAndExportPrivateKey(t *testing.T) {
	if _, err := exec.LookPath("ssh-keygen"); err != nil {
		t.Skip("ssh-keygen is unavailable")
	}

	root := t.TempDir()
	source := filepath.Join(root, "source-key")
	command := exec.Command("ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-C", "test", "-f", source)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("generate test key: %v: %s", err, output)
	}
	original, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}

	service := &keyService{
		keysDir:      filepath.Join(root, "managed", "keys"),
		manifestPath: filepath.Join(root, "managed", "keys.json"),
	}
	key, err := service.ImportPrivateKey("test private", source)
	if err != nil {
		t.Fatal(err)
	}
	if !key.HasPrivateKey() {
		t.Fatal("private import does not have a private key")
	}
	managedPath, err := service.PrivateKeyPath(key.ID)
	if err != nil {
		t.Fatal(err)
	}
	if mode := fileMode(t, managedPath); mode.Perm() != 0o600 {
		t.Fatalf("managed private key mode = %o", mode.Perm())
	}

	destination := filepath.Join(root, "restored", "id_ed25519")
	if err := service.ExportKey(key.ID, destination); err != nil {
		t.Fatal(err)
	}
	restored, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(restored, original) {
		t.Fatal("restored private key differs from imported source")
	}
	if _, err := os.Stat(destination + ".pub"); err != nil {
		t.Fatalf("restored public key: %v", err)
	}
	if mode := fileMode(t, destination); mode.Perm() != 0o600 {
		t.Fatalf("restored private key mode = %o", mode.Perm())
	}
	if _, err := os.Stat(source); err != nil {
		t.Fatalf("source private key was modified or removed: %v", err)
	}
}

func fileMode(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode()
}
