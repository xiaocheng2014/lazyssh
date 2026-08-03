package services

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/xiaocheng2014/lazyssh/internal/core/domain"
	"github.com/xiaocheng2014/lazyssh/internal/core/ports"
	"golang.org/x/crypto/ssh"
)

type keyManifest struct {
	Version int                 `json:"version"`
	Keys    []domain.ManagedKey `json:"keys"`
}

type keyService struct {
	mu           sync.Mutex
	keysDir      string
	manifestPath string
	syncVault    func() error
}

func NewKeyService(keysDir, manifestPath string, syncVault func() error) ports.KeyService {
	return &keyService{keysDir: keysDir, manifestPath: manifestPath, syncVault: syncVault}
}

func (s *keyService) ListKeys() ([]domain.ManagedKey, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	manifest, err := s.loadManifest()
	if err != nil {
		return nil, err
	}
	keys := append([]domain.ManagedKey(nil), manifest.Keys...)
	sort.Slice(keys, func(i, j int) bool { return strings.ToLower(keys[i].Name) < strings.ToLower(keys[j].Name) })
	return keys, nil
}

func (s *keyService) ImportPrivateKey(name, sourcePath string) (domain.ManagedKey, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	name, sourcePath, err := validateKeyImport(name, sourcePath)
	if err != nil {
		return domain.ManagedKey{}, err
	}

	// ssh-keygen handles all OpenSSH private key formats and prompts on the
	// controlling terminal when the imported key already has a passphrase.
	command := exec.Command("ssh-keygen", "-y", "-f", sourcePath)
	command.Stdin = os.Stdin
	command.Stderr = os.Stderr
	publicOutput, err := command.Output()
	if err != nil {
		return domain.ManagedKey{}, fmt.Errorf("read public key from private key: %w", err)
	}
	publicLine := strings.TrimSpace(string(publicOutput))
	keyType, fingerprint, id, err := inspectPublicKey(publicLine)
	if err != nil {
		return domain.ManagedKey{}, fmt.Errorf("invalid private key: %w", err)
	}

	manifest, err := s.loadManifest()
	if err != nil {
		return domain.ManagedKey{}, err
	}
	if err := ensureUniqueKey(manifest.Keys, id, name); err != nil {
		return domain.ManagedKey{}, err
	}
	if err := os.MkdirAll(s.keysDir, 0o700); err != nil {
		return domain.ManagedKey{}, fmt.Errorf("create managed key directory: %w", err)
	}
	privateFile := "key-" + id
	publicFile := privateFile + ".pub"
	privatePath := filepath.Join(s.keysDir, privateFile)
	publicPath := filepath.Join(s.keysDir, publicFile)
	if err := copyPrivateKey(sourcePath, privatePath); err != nil {
		return domain.ManagedKey{}, err
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(privatePath)
			_ = os.Remove(publicPath)
		}
	}()
	if err := os.WriteFile(publicPath, []byte(publicLine+"\n"), 0o600); err != nil {
		return domain.ManagedKey{}, fmt.Errorf("write managed public key: %w", err)
	}

	managedKey := domain.ManagedKey{
		ID:          id,
		Name:        name,
		KeyType:     keyType,
		Fingerprint: fingerprint,
		PrivateFile: privateFile,
		PublicFile:  publicFile,
		CreatedAt:   time.Now().UTC(),
	}
	manifest.Keys = append(manifest.Keys, managedKey)
	if err := s.saveManifest(manifest); err != nil {
		return domain.ManagedKey{}, err
	}
	cleanup = false
	if err := s.sync(); err != nil {
		return domain.ManagedKey{}, err
	}
	return managedKey, nil
}

func (s *keyService) ImportPublicKey(name, sourcePath string) (domain.ManagedKey, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	name, sourcePath, err := validateKeyImport(name, sourcePath)
	if err != nil {
		return domain.ManagedKey{}, err
	}
	data, err := os.ReadFile(sourcePath)
	if err != nil {
		return domain.ManagedKey{}, fmt.Errorf("read public key: %w", err)
	}
	publicLine := firstPublicKeyLine(string(data))
	keyType, fingerprint, id, err := inspectPublicKey(publicLine)
	if err != nil {
		return domain.ManagedKey{}, fmt.Errorf("invalid public key: %w", err)
	}
	manifest, err := s.loadManifest()
	if err != nil {
		return domain.ManagedKey{}, err
	}
	if err := ensureUniqueKey(manifest.Keys, id, name); err != nil {
		return domain.ManagedKey{}, err
	}
	if err := os.MkdirAll(s.keysDir, 0o700); err != nil {
		return domain.ManagedKey{}, fmt.Errorf("create managed key directory: %w", err)
	}
	publicFile := "key-" + id + ".pub"
	publicPath := filepath.Join(s.keysDir, publicFile)
	if err := os.WriteFile(publicPath, []byte(publicLine+"\n"), 0o600); err != nil {
		return domain.ManagedKey{}, fmt.Errorf("write managed public key: %w", err)
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(publicPath)
		}
	}()
	managedKey := domain.ManagedKey{
		ID:          id,
		Name:        name,
		KeyType:     keyType,
		Fingerprint: fingerprint,
		PublicFile:  publicFile,
		CreatedAt:   time.Now().UTC(),
	}
	manifest.Keys = append(manifest.Keys, managedKey)
	if err := s.saveManifest(manifest); err != nil {
		return domain.ManagedKey{}, err
	}
	cleanup = false
	if err := s.sync(); err != nil {
		return domain.ManagedKey{}, err
	}
	return managedKey, nil
}

func (s *keyService) DeleteKey(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	manifest, err := s.loadManifest()
	if err != nil {
		return err
	}
	index := -1
	var target domain.ManagedKey
	for i, key := range manifest.Keys {
		if key.ID == id {
			index = i
			target = key
			break
		}
	}
	if index < 0 {
		return fmt.Errorf("managed key %q not found", id)
	}
	manifest.Keys = append(manifest.Keys[:index], manifest.Keys[index+1:]...)
	if err := s.saveManifest(manifest); err != nil {
		return err
	}
	if target.PrivateFile != "" {
		_ = os.Remove(filepath.Join(s.keysDir, target.PrivateFile))
	}
	_ = os.Remove(filepath.Join(s.keysDir, target.PublicFile))
	return s.sync()
}

func (s *keyService) PrivateKeyPath(id string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	manifest, err := s.loadManifest()
	if err != nil {
		return "", err
	}
	for _, key := range manifest.Keys {
		if key.ID != id {
			continue
		}
		if key.PrivateFile == "" {
			return "", fmt.Errorf("managed key %q contains only a public key", key.Name)
		}
		path := filepath.Join(s.keysDir, key.PrivateFile)
		if _, err := os.Stat(path); err != nil {
			return "", fmt.Errorf("managed private key %q is unavailable: %w", key.Name, err)
		}
		return path, nil
	}
	return "", fmt.Errorf("managed key %q not found", id)
}

func (s *keyService) PublicKey(id string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	manifest, err := s.loadManifest()
	if err != nil {
		return "", err
	}
	for _, key := range manifest.Keys {
		if key.ID != id {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.keysDir, key.PublicFile))
		if err != nil {
			return "", fmt.Errorf("read managed public key: %w", err)
		}
		return strings.TrimSpace(string(data)), nil
	}
	return "", fmt.Errorf("managed key %q not found", id)
}

// ExportKey restores a managed key to an explicit user-selected path. For a
// private key, the public key is also written to destination + ".pub".
func (s *keyService) ExportKey(id, destination string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	destination, err := expandUserPath(strings.TrimSpace(destination))
	if err != nil {
		return err
	}
	if destination == "" {
		return errors.New("restore destination is required")
	}
	manifest, err := s.loadManifest()
	if err != nil {
		return err
	}
	var target *domain.ManagedKey
	for i := range manifest.Keys {
		if manifest.Keys[i].ID == id {
			target = &manifest.Keys[i]
			break
		}
	}
	if target == nil {
		return fmt.Errorf("managed key %q not found", id)
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return fmt.Errorf("create restore directory: %w", err)
	}
	if target.PrivateFile == "" {
		return copyExportFile(filepath.Join(s.keysDir, target.PublicFile), destination, 0o600)
	}
	if err := copyExportFile(filepath.Join(s.keysDir, target.PrivateFile), destination, 0o600); err != nil {
		return err
	}
	if err := copyExportFile(filepath.Join(s.keysDir, target.PublicFile), destination+".pub", 0o600); err != nil {
		_ = os.Remove(destination)
		return err
	}
	return nil
}

func (s *keyService) loadManifest() (keyManifest, error) {
	manifest := keyManifest{Version: 1, Keys: []domain.ManagedKey{}}
	data, err := os.ReadFile(s.manifestPath)
	if os.IsNotExist(err) {
		return manifest, nil
	}
	if err != nil {
		return manifest, fmt.Errorf("read key manifest: %w", err)
	}
	if len(data) == 0 {
		return manifest, nil
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return manifest, fmt.Errorf("parse key manifest: %w", err)
	}
	if manifest.Version != 1 {
		return manifest, fmt.Errorf("unsupported key manifest version %d", manifest.Version)
	}
	return manifest, nil
}

func (s *keyService) saveManifest(manifest keyManifest) error {
	if err := os.MkdirAll(filepath.Dir(s.manifestPath), 0o700); err != nil {
		return fmt.Errorf("create key manifest directory: %w", err)
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("encode key manifest: %w", err)
	}
	temp, err := os.CreateTemp(filepath.Dir(s.manifestPath), ".keys-*.tmp")
	if err != nil {
		return fmt.Errorf("create key manifest temporary file: %w", err)
	}
	tempPath := temp.Name()
	defer func() { _ = os.Remove(tempPath) }()
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return err
	}
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempPath, s.manifestPath); err != nil {
		return fmt.Errorf("replace key manifest: %w", err)
	}
	return nil
}

func (s *keyService) sync() error {
	if s.syncVault == nil {
		return nil
	}
	if err := s.syncVault(); err != nil {
		return fmt.Errorf("save encrypted vault: %w", err)
	}
	return nil
}

func validateKeyImport(name, sourcePath string) (string, string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", "", errors.New("key name is required")
	}
	if len(name) > 80 {
		return "", "", errors.New("key name must not exceed 80 characters")
	}
	sourcePath = strings.TrimSpace(sourcePath)
	if sourcePath == "" {
		return "", "", errors.New("key file path is required")
	}
	sourcePath, err := expandUserPath(sourcePath)
	if err != nil {
		return "", "", err
	}
	resolved, err := filepath.Abs(sourcePath)
	if err != nil {
		return "", "", fmt.Errorf("resolve key file path: %w", err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", "", fmt.Errorf("inspect key file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", "", errors.New("key path must point to a regular file")
	}
	return name, resolved, nil
}

func expandUserPath(path string) (string, error) {
	if path == "~" || strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\`) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve user home directory: %w", err)
		}
		if path == "~" {
			return home, nil
		}
		return filepath.Join(home, path[2:]), nil
	}
	return path, nil
}

func firstPublicKeyLine(content string) string {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			return line
		}
	}
	return ""
}

func inspectPublicKey(publicLine string) (keyType, fingerprint, id string, err error) {
	publicKey, _, _, _, err := ssh.ParseAuthorizedKey([]byte(publicLine))
	if err != nil {
		return "", "", "", errors.New("invalid OpenSSH public key data")
	}
	blob := publicKey.Marshal()
	hash := sha256.Sum256(blob)
	return publicKey.Type(), ssh.FingerprintSHA256(publicKey), hex.EncodeToString(hash[:8]), nil
}

func ensureUniqueKey(keys []domain.ManagedKey, id, name string) error {
	for _, key := range keys {
		if key.ID == id {
			return fmt.Errorf("this key is already imported as %q", key.Name)
		}
		if strings.EqualFold(key.Name, name) {
			return fmt.Errorf("a managed key named %q already exists", key.Name)
		}
	}
	return nil
}

func copyPrivateKey(source, destination string) error {
	input, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("open private key: %w", err)
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("create managed private key: %w", err)
	}
	_, copyErr := io.Copy(output, input)
	syncErr := output.Sync()
	closeErr := output.Close()
	if err := errors.Join(copyErr, syncErr, closeErr); err != nil {
		return fmt.Errorf("copy private key: %w", err)
	}
	return nil
}

func copyExportFile(source, destination string, mode os.FileMode) error {
	input, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("open managed key for restore: %w", err)
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		if os.IsExist(err) {
			return fmt.Errorf("restore destination already exists: %s", destination)
		}
		return fmt.Errorf("create restored key: %w", err)
	}
	_, copyErr := io.Copy(output, input)
	syncErr := output.Sync()
	closeErr := output.Close()
	if err := errors.Join(copyErr, syncErr, closeErr); err != nil {
		_ = os.Remove(destination)
		return fmt.Errorf("restore key: %w", err)
	}
	return nil
}
