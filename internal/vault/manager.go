package vault

import (
	"archive/tar"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"filippo.io/age"
)

const (
	BundleName   = "lazyssh.bundle.age"
	ManifestName = "vault.json"
	ConfigName   = "config"
	MetadataName = "metadata.json"
	KeysDirName  = "keys"
	// LocalPasswordName is intentionally excluded from the encrypted bundle and
	// Git. It lets a trusted device unlock the vault without prompting each run.
	LocalPasswordName = ".vault-password"
	lockName          = ".lazyssh.lock"
)

const (
	desktopScryptWorkFactor = 18
	ishScryptWorkFactor     = 15
	minScryptWorkFactor     = 10
	maxScryptWorkFactor     = 22
)

var scryptWorkFactor = recommendedScryptWorkFactor(runtime.GOOS, runtime.GOARCH)

const lockWaitTimeout = 3 * time.Second

var ErrVaultChanged = errors.New("加密仓库已被另一个 LazySSH 窗口更新，请重新打开当前窗口后再修改")

type Manifest struct {
	Version          int       `json:"version"`
	UpdatedAt        time.Time `json:"updated_at"`
	ScryptWorkFactor int       `json:"scrypt_work_factor,omitempty"`
}

// Manager owns an unlocked, temporary working directory and persists it as a
// single password-encrypted age bundle.
type Manager struct {
	mu        sync.Mutex
	vaultDir  string
	vaultPath string
	workDir   string
	password  []byte
	closed    bool
	lockPath  string
	revision  [sha256.Size]byte
	hasBundle bool
	// scryptWorkFactor is stored in the encrypted manifest so a vault created
	// for iSH remains usable there after another platform re-encrypts it.
	scryptWorkFactor int
}

func BundlePath(vaultDir string) string {
	return filepath.Join(vaultDir, BundleName)
}

func Exists(vaultDir string) bool {
	_, err := os.Stat(BundlePath(vaultDir))
	return err == nil
}

func LocalPasswordPath(vaultDir string) string {
	return filepath.Join(vaultDir, LocalPasswordName)
}

// LoadLocalPassword returns the password stored only on this device.
func LoadLocalPassword(vaultDir string) ([]byte, bool, error) {
	path := LocalPasswordPath(vaultDir)
	password, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("read local vault password: %w", err)
	}
	if len(password) == 0 {
		return nil, false, errors.New("local vault password file is empty")
	}
	if err := os.Chmod(path, 0o600); err != nil {
		clear(password)
		return nil, false, fmt.Errorf("secure local vault password: %w", err)
	}
	return password, true, nil
}

// Create creates a new encrypted vault. Initial files are copied by archive
// name, for example map[ConfigName]="/home/user/.ssh/config".
func Create(vaultDir string, password []byte, initialFiles map[string]string) (*Manager, error) {
	if len(password) == 0 {
		return nil, errors.New("vault password must not be empty")
	}
	if err := os.MkdirAll(vaultDir, 0o700); err != nil {
		return nil, fmt.Errorf("create vault directory: %w", err)
	}
	if Exists(vaultDir) {
		return nil, fmt.Errorf("vault already exists at %s", BundlePath(vaultDir))
	}

	manager, err := newManager(vaultDir, password)
	if err != nil {
		return nil, err
	}
	cleanup := true
	defer func() {
		if cleanup {
			manager.cleanupRuntime()
		}
	}()

	if err := os.MkdirAll(manager.Path(KeysDirName), 0o700); err != nil {
		return nil, fmt.Errorf("create vault key directory: %w", err)
	}
	if err := copyExistingVaultContents(vaultDir, manager.workDir); err != nil {
		return nil, err
	}
	for archiveName, source := range initialFiles {
		if source == "" {
			continue
		}
		if err := copyOptionalFile(source, manager.Path(archiveName)); err != nil {
			return nil, err
		}
	}
	if err := manager.saveLocked(); err != nil {
		return nil, err
	}

	cleanup = false
	return manager, nil
}

func Unlock(vaultDir string, password []byte) (*Manager, error) {
	if len(password) == 0 {
		return nil, errors.New("vault password must not be empty")
	}
	manager, err := newManager(vaultDir, password)
	if err != nil {
		return nil, err
	}
	if err := acquireLockWithRetry(manager.lockPath); err != nil {
		manager.cleanupRuntime()
		return nil, err
	}
	defer releaseLock(manager.lockPath)
	revision, err := hashBundle(manager.vaultPath)
	if err != nil {
		manager.cleanupRuntime()
		return nil, fmt.Errorf("read encrypted vault revision: %w", err)
	}
	if err := extractBundle(manager.vaultPath, manager.workDir, password); err != nil {
		manager.cleanupRuntime()
		return nil, fmt.Errorf("unlock vault: %w", err)
	}
	if err := os.MkdirAll(manager.Path(KeysDirName), 0o700); err != nil {
		manager.cleanupRuntime()
		return nil, fmt.Errorf("create vault key directory: %w", err)
	}
	manager.loadScryptWorkFactor()
	manager.revision = revision
	manager.hasBundle = true
	return manager, nil
}

func newManager(vaultDir string, password []byte) (*Manager, error) {
	lockPath := filepath.Join(vaultDir, lockName)
	workDir, err := os.MkdirTemp("", "lazyssh-runtime-")
	if err != nil {
		return nil, fmt.Errorf("create vault runtime directory: %w", err)
	}
	if err := os.Chmod(workDir, 0o700); err != nil {
		_ = os.RemoveAll(workDir)
		return nil, fmt.Errorf("secure vault runtime directory: %w", err)
	}
	return &Manager{
		vaultDir:         vaultDir,
		vaultPath:        BundlePath(vaultDir),
		workDir:          workDir,
		password:         append([]byte(nil), password...),
		lockPath:         lockPath,
		scryptWorkFactor: scryptWorkFactor,
	}, nil
}

func recommendedScryptWorkFactor(goos, goarch string) int {
	// The released linux/386 build targets iSH, whose interpreted x86 runtime
	// makes age's desktop default appear to hang and can exhaust its memory.
	if goos == "linux" && goarch == "386" {
		return ishScryptWorkFactor
	}
	return desktopScryptWorkFactor
}

func (m *Manager) loadScryptWorkFactor() {
	data, err := os.ReadFile(m.Path(ManifestName))
	if err != nil {
		return
	}
	var manifest Manifest
	if json.Unmarshal(data, &manifest) != nil {
		return
	}
	if manifest.ScryptWorkFactor >= minScryptWorkFactor && manifest.ScryptWorkFactor <= maxScryptWorkFactor {
		m.scryptWorkFactor = manifest.ScryptWorkFactor
	}
}

func acquireLockWithRetry(lockPath string) error {
	deadline := time.Now().Add(lockWaitTimeout)
	for {
		err := acquireLock(lockPath)
		if err == nil {
			return nil
		}
		if !errors.Is(err, os.ErrExist) || time.Now().After(deadline) {
			return err
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func releaseLock(lockPath string) {
	_ = os.Remove(lockPath)
}

func acquireLock(lockPath string) error {
	for attempt := 0; attempt < 2; attempt++ {
		lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			if _, err := fmt.Fprintf(lockFile, "%d\n", os.Getpid()); err != nil {
				_ = lockFile.Close()
				_ = os.Remove(lockPath)
				return fmt.Errorf("write vault lock: %w", err)
			}
			if err := lockFile.Close(); err != nil {
				_ = os.Remove(lockPath)
				return fmt.Errorf("close vault lock: %w", err)
			}
			return nil
		}
		if !os.IsExist(err) {
			return fmt.Errorf("create vault lock: %w", err)
		}

		stale, lockPID, inspectErr := inspectLock(lockPath)
		if inspectErr != nil {
			return inspectErr
		}
		if !stale {
			return fmt.Errorf("another LazySSH process is saving the vault (process %d, lock: %s): %w", lockPID, lockPath, os.ErrExist)
		}
		if err := os.Remove(lockPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove stale vault lock %s: %w", lockPath, err)
		}
	}
	return fmt.Errorf("could not acquire vault lock at %s", lockPath)
}

func inspectLock(lockPath string) (stale bool, pid int, err error) {
	data, err := os.ReadFile(lockPath)
	if err != nil {
		if os.IsNotExist(err) {
			return true, 0, nil
		}
		return false, 0, fmt.Errorf("read vault lock %s: %w", lockPath, err)
	}
	pid, err = strconv.Atoi(strings.TrimSpace(string(data)))
	if err == nil && pid > 0 {
		return !processExists(pid), pid, nil
	}

	info, statErr := os.Stat(lockPath)
	if statErr != nil {
		if os.IsNotExist(statErr) {
			return true, 0, nil
		}
		return false, 0, fmt.Errorf("inspect malformed vault lock %s: %w", lockPath, statErr)
	}
	// Another process may be between creating the lock file and writing its PID.
	// Treat a fresh malformed lock as active; an older one is safe to recover.
	return time.Since(info.ModTime()) > 5*time.Second, 0, nil
}

func (m *Manager) Path(name string) string {
	return filepath.Join(m.workDir, name)
}

func (m *Manager) WorkDir() string {
	return m.workDir
}

func (m *Manager) VaultPath() string {
	return m.vaultPath
}

func (m *Manager) LocalPasswordPath() string {
	return LocalPasswordPath(m.vaultDir)
}

// SaveLocalPassword stores the active password outside the encrypted bundle.
// The file is owner-only and excluded from Git by the vault directory's
// allow-list .gitignore.
func (m *Manager) SaveLocalPassword() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return errors.New("vault is closed")
	}
	return writeLocalPassword(m.vaultDir, m.password)
}

// SealPlaintext removes the plaintext files that were imported from the vault
// directory, leaving only the encrypted bundle and Git support files.
func (m *Manager) SealPlaintext() error {
	entries, err := os.ReadDir(m.vaultDir)
	if err != nil {
		return fmt.Errorf("read vault directory before sealing: %w", err)
	}
	for _, entry := range entries {
		switch entry.Name() {
		case BundleName, LocalPasswordName, lockName, ".git", ".gitignore", ".gitattributes", "README.md":
			continue
		}
		if strings.HasPrefix(entry.Name(), ".lazyssh-vault-") || strings.HasPrefix(entry.Name(), BundleName+".") {
			continue
		}
		if err := os.RemoveAll(filepath.Join(m.vaultDir, entry.Name())); err != nil {
			return fmt.Errorf("remove sealed plaintext %s: %w", entry.Name(), err)
		}
	}
	return nil
}

func (m *Manager) Save() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return errors.New("vault is closed")
	}
	return m.saveLocked()
}

func (m *Manager) ChangePassword(newPassword []byte) error {
	if len(newPassword) < 10 {
		return errors.New("vault password must contain at least 10 characters")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return errors.New("vault is closed")
	}
	previous := m.password
	m.password = append([]byte(nil), newPassword...)
	if err := m.saveLocked(); err != nil {
		for i := range m.password {
			m.password[i] = 0
		}
		m.password = previous
		return fmt.Errorf("re-encrypt vault with new password: %w", err)
	}
	if err := writeLocalPassword(m.vaultDir, m.password); err != nil {
		for i := range m.password {
			m.password[i] = 0
		}
		m.password = previous
		rollbackErr := m.saveLocked()
		if rollbackErr != nil {
			return errors.Join(
				fmt.Errorf("save new local vault password: %w", err),
				fmt.Errorf("restore vault encryption with previous password: %w", rollbackErr),
			)
		}
		return fmt.Errorf("save new local vault password; vault remains on the previous password: %w", err)
	}
	for i := range previous {
		previous[i] = 0
	}
	return nil
}

// VerifyPassword checks a candidate against the encrypted bundle without
// changing the active password or any vault content.
func (m *Manager) VerifyPassword(password []byte) error {
	if len(password) == 0 {
		return errors.New("vault password must not be empty")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return errors.New("vault is closed")
	}
	if err := verifyBundle(m.vaultPath, password); err != nil {
		return fmt.Errorf("密码不正确或加密仓库已损坏：%w", err)
	}
	return nil
}

func (m *Manager) saveLocked() error {
	if err := acquireLockWithRetry(m.lockPath); err != nil {
		return err
	}
	defer releaseLock(m.lockPath)

	currentExists := Exists(m.vaultDir)
	if m.hasBundle != currentExists {
		return ErrVaultChanged
	}
	if currentExists {
		currentRevision, err := hashBundle(m.vaultPath)
		if err != nil {
			return fmt.Errorf("read current encrypted vault revision: %w", err)
		}
		if currentRevision != m.revision {
			return ErrVaultChanged
		}
	}

	manifest := Manifest{
		Version:          1,
		UpdatedAt:        time.Now().UTC(),
		ScryptWorkFactor: m.scryptWorkFactor,
	}
	manifestData, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("encode vault manifest: %w", err)
	}
	if err := os.WriteFile(m.Path(ManifestName), manifestData, 0o600); err != nil {
		return fmt.Errorf("write vault manifest: %w", err)
	}

	tempFile, err := os.CreateTemp(m.vaultDir, ".lazyssh-vault-*.tmp")
	if err != nil {
		return fmt.Errorf("create encrypted vault temporary file: %w", err)
	}
	tempPath := tempFile.Name()
	defer func() { _ = os.Remove(tempPath) }()
	if err := tempFile.Chmod(0o600); err != nil {
		_ = tempFile.Close()
		return fmt.Errorf("secure encrypted vault temporary file: %w", err)
	}

	recipient, err := age.NewScryptRecipient(string(m.password))
	if err != nil {
		_ = tempFile.Close()
		return fmt.Errorf("initialize vault encryption: %w", err)
	}
	recipient.SetWorkFactor(m.scryptWorkFactor)
	encryptedWriter, err := age.Encrypt(tempFile, recipient)
	if err != nil {
		_ = tempFile.Close()
		return fmt.Errorf("initialize encrypted vault stream: %w", err)
	}
	if err := writeArchive(encryptedWriter, m.workDir); err != nil {
		_ = encryptedWriter.Close()
		_ = tempFile.Close()
		return err
	}
	if err := encryptedWriter.Close(); err != nil {
		_ = tempFile.Close()
		return fmt.Errorf("finalize encrypted vault: %w", err)
	}
	if err := tempFile.Sync(); err != nil {
		_ = tempFile.Close()
		return fmt.Errorf("sync encrypted vault: %w", err)
	}
	if err := tempFile.Close(); err != nil {
		return fmt.Errorf("close encrypted vault: %w", err)
	}
	if err := verifyBundle(tempPath, m.password); err != nil {
		return fmt.Errorf("verify encrypted vault: %w", err)
	}
	newRevision, err := hashBundle(tempPath)
	if err != nil {
		return fmt.Errorf("hash encrypted vault: %w", err)
	}
	if err := replaceBundle(tempPath, m.vaultPath); err != nil {
		return err
	}
	m.revision = newRevision
	m.hasBundle = true
	return nil
}

// Close saves the current state and removes the decrypted runtime directory.
// If saving fails, the runtime directory is retained and included in the error.
func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil
	}
	if err := m.saveLocked(); err != nil {
		return fmt.Errorf("save vault before close (recovery files retained at %s): %w", m.workDir, err)
	}
	m.closed = true
	for i := range m.password {
		m.password[i] = 0
	}
	return m.cleanupRuntime()
}

// CloseReadOnly removes decrypted runtime state without creating a new
// randomized encrypted bundle. Callers must use it only for read-only work or
// after mutations have already been synchronized through Save.
func (m *Manager) CloseReadOnly() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil
	}
	m.closed = true
	for i := range m.password {
		m.password[i] = 0
	}
	return m.cleanupRuntime()
}

func (m *Manager) removeWorkDir() error {
	if m.workDir == "" {
		return nil
	}
	if err := os.RemoveAll(m.workDir); err != nil {
		return fmt.Errorf("remove decrypted vault runtime directory: %w", err)
	}
	return nil
}

func (m *Manager) cleanupRuntime() error {
	return m.removeWorkDir()
}

func hashBundle(path string) ([sha256.Size]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return [sha256.Size]byte{}, err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return [sha256.Size]byte{}, err
	}
	var revision [sha256.Size]byte
	copy(revision[:], hash.Sum(nil))
	return revision, nil
}

func copyOptionalFile(source, destination string) error {
	if _, err := os.Stat(destination); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect initial vault destination %s: %w", destination, err)
	}
	sourceFile, err := os.Open(source)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("open initial vault file %s: %w", source, err)
	}
	defer sourceFile.Close()
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return fmt.Errorf("create initial vault directory: %w", err)
	}
	destinationFile, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("create initial vault file %s: %w", destination, err)
	}
	if _, err := io.Copy(destinationFile, sourceFile); err != nil {
		_ = destinationFile.Close()
		return fmt.Errorf("copy initial vault file %s: %w", source, err)
	}
	if err := destinationFile.Sync(); err != nil {
		_ = destinationFile.Close()
		return fmt.Errorf("sync initial vault file %s: %w", destination, err)
	}
	return destinationFile.Close()
}

func copyExistingVaultContents(sourceRoot, destinationRoot string) error {
	return filepath.WalkDir(sourceRoot, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == sourceRoot {
			return nil
		}
		relative, err := filepath.Rel(sourceRoot, path)
		if err != nil {
			return err
		}
		firstPart := strings.Split(filepath.ToSlash(relative), "/")[0]
		if firstPart == ".git" || firstPart == ".gitignore" || firstPart == ".gitattributes" || firstPart == "README.md" || firstPart == BundleName || strings.HasPrefix(firstPart, BundleName+".") || strings.HasPrefix(firstPart, LocalPasswordName) || firstPart == lockName || strings.HasPrefix(firstPart, ".lazyssh-vault-") {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("cannot import symbolic link into vault: %s", path)
		}
		target := filepath.Join(destinationRoot, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("cannot import unsupported file into vault: %s", path)
		}
		return copyOptionalFile(path, target)
	})
}

func writeLocalPassword(vaultDir string, password []byte) error {
	if len(password) == 0 {
		return errors.New("local vault password must not be empty")
	}
	if err := os.MkdirAll(vaultDir, 0o700); err != nil {
		return fmt.Errorf("create local password directory: %w", err)
	}
	temp, err := os.CreateTemp(vaultDir, ".vault-password-*.tmp")
	if err != nil {
		return fmt.Errorf("create local password temporary file: %w", err)
	}
	tempPath := temp.Name()
	defer func() { _ = os.Remove(tempPath) }()
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return fmt.Errorf("secure local password temporary file: %w", err)
	}
	if _, err := temp.Write(password); err != nil {
		_ = temp.Close()
		return fmt.Errorf("write local password temporary file: %w", err)
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return fmt.Errorf("sync local password temporary file: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close local password temporary file: %w", err)
	}
	if err := replaceLocalFile(tempPath, LocalPasswordPath(vaultDir)); err != nil {
		return fmt.Errorf("replace local password file: %w", err)
	}
	return nil
}

func replaceLocalFile(tempPath, destination string) error {
	backupPath := destination + ".previous"
	_ = os.Remove(backupPath)
	hadExisting := false
	if _, err := os.Stat(destination); err == nil {
		hadExisting = true
		if err := os.Rename(destination, backupPath); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.Rename(tempPath, destination); err != nil {
		if hadExisting {
			_ = os.Rename(backupPath, destination)
		}
		return err
	}
	if hadExisting {
		_ = os.Remove(backupPath)
	}
	return nil
}

func writeArchive(destination io.Writer, root string) error {
	tarWriter := tar.NewWriter(destination)
	walkErr := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == root {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("vault does not support symbolic links: %s", path)
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = relative
		if entry.IsDir() {
			header.Mode = 0o700
		} else if info.Mode().IsRegular() {
			header.Mode = 0o600
		} else {
			return fmt.Errorf("unsupported vault file type: %s", path)
		}
		if err := tarWriter.WriteHeader(header); err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(tarWriter, file)
		closeErr := file.Close()
		return errors.Join(copyErr, closeErr)
	})
	closeErr := tarWriter.Close()
	if err := errors.Join(walkErr, closeErr); err != nil {
		return fmt.Errorf("archive vault directory: %w", err)
	}
	return nil
}

func extractBundle(bundlePath, destination string, password []byte) error {
	file, err := os.Open(bundlePath)
	if err != nil {
		return err
	}
	defer file.Close()
	identity, err := age.NewScryptIdentity(string(password))
	if err != nil {
		return err
	}
	decrypted, err := age.Decrypt(file, identity)
	if err != nil {
		return err
	}
	reader := tar.NewReader(decrypted)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		target, err := secureArchivePath(destination, header.Name)
		if err != nil {
			return err
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o700); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
				return err
			}
			output, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(output, reader)
			closeErr := output.Close()
			if err := errors.Join(copyErr, closeErr); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unsupported archive entry type for %s", header.Name)
		}
	}
	return nil
}

func verifyBundle(bundlePath string, password []byte) error {
	file, err := os.Open(bundlePath)
	if err != nil {
		return err
	}
	defer file.Close()
	identity, err := age.NewScryptIdentity(string(password))
	if err != nil {
		return err
	}
	decrypted, err := age.Decrypt(file, identity)
	if err != nil {
		return err
	}
	reader := tar.NewReader(decrypted)
	for {
		_, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if _, err := io.Copy(io.Discard, reader); err != nil {
			return err
		}
	}
}

func secureArchivePath(root, name string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(name))
	if clean == "." || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("invalid vault archive path %q", name)
	}
	target := filepath.Join(root, clean)
	relative, err := filepath.Rel(root, target)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("vault archive path escapes runtime directory: %q", name)
	}
	return target, nil
}

func replaceBundle(tempPath, destination string) error {
	backupPath := destination + ".previous"
	_ = os.Remove(backupPath)
	hadExisting := false
	if _, err := os.Stat(destination); err == nil {
		hadExisting = true
		if err := os.Rename(destination, backupPath); err != nil {
			return fmt.Errorf("backup previous encrypted vault: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect previous encrypted vault: %w", err)
	}
	if err := os.Rename(tempPath, destination); err != nil {
		if hadExisting {
			_ = os.Rename(backupPath, destination)
		}
		return fmt.Errorf("replace encrypted vault: %w", err)
	}
	if hadExisting {
		_ = os.Remove(backupPath)
	}
	return nil
}
