package services

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xiaocheng2014/lazyssh/internal/core/domain"
	"github.com/xiaocheng2014/lazyssh/internal/core/ports"
)

func TestTransferPathValidation(t *testing.T) {
	for _, path := range []string{"", "relative/file", "/", "/tmp/", "/tmp/line\nbreak", "/tmp/nul\x00"} {
		if err := validateRemoteTransferPath(path); err == nil {
			t.Errorf("validateRemoteTransferPath(%q) succeeded", path)
		}
	}
	for _, path := range []string{"/tmp/a b", "/tmp/中文's file", "/tmp/a;touch-no"} {
		if err := validateRemoteTransferPath(path); err != nil {
			t.Errorf("validateRemoteTransferPath(%q): %v", path, err)
		}
	}
	if got, want := quotePOSIXShell("/tmp/a'b"), "'/tmp/a'\\''b'"; got != want {
		t.Fatalf("quotePOSIXShell = %q, want %q", got, want)
	}
}

func TestUploadUsesSelectedServerAndRefusesOverwrite(t *testing.T) {
	binDir := t.TempDir()
	sshScript := "#!/bin/sh\nexit \"${LAZYSSH_TEST_SSH_EXIT:-43}\"\n"
	scpScript := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$LAZYSSH_TEST_SCP_ARGS\"\n"
	if err := os.WriteFile(filepath.Join(binDir, "ssh"), []byte(sshScript), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "scp"), []byte(scpScript), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	argFile := filepath.Join(t.TempDir(), "args")
	t.Setenv("LAZYSSH_TEST_SCP_ARGS", argFile)

	local := filepath.Join(t.TempDir(), "local file.txt")
	if err := os.WriteFile(local, []byte("test"), 0o600); err != nil {
		t.Fatal(err)
	}
	service := &serverService{
		serverRepository: &serverServiceTestRepo{servers: []domain.Server{{Alias: "生产数据库", ManagedKeyID: "key-1"}}},
		keyService:       &serverServiceTestKeys{path: "/runtime/keys/key-1"},
		sshConfigPath:    "/runtime/config",
	}
	remote := "/tmp/中文's file.txt"
	if err := service.Upload("生产数据库", local, remote, ports.TransferOptions{}); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(argFile)
	if err != nil {
		t.Fatal(err)
	}
	args := string(content)
	for _, expected := range []string{"-F\n/runtime/config\n", "-i\n/runtime/keys/key-1\n", local + "\n", domain.SSHConnectionAlias("生产数据库") + ":" + remote + "\n"} {
		if !strings.Contains(args, expected) {
			t.Errorf("scp args %q do not include %q", args, expected)
		}
	}
	t.Setenv("LAZYSSH_TEST_SSH_EXIT", "42")
	if err := os.Remove(argFile); err != nil {
		t.Fatal(err)
	}
	if err := service.Upload("生产数据库", local, remote, ports.TransferOptions{}); err == nil || !strings.Contains(err.Error(), "--overwrite") {
		t.Fatalf("existing remote target: got %v, want overwrite error", err)
	}
	if _, err := os.Stat(argFile); !os.IsNotExist(err) {
		t.Fatalf("scp ran despite existing remote target: %v", err)
	}
	if err := service.Upload("生产数据库", local, remote, ports.TransferOptions{Overwrite: true}); err != nil {
		t.Fatalf("explicit overwrite failed: %v", err)
	}
	t.Setenv("LAZYSSH_TEST_SSH_EXIT", "45")
	if err := service.Upload("生产数据库", local, remote, ports.TransferOptions{Overwrite: true}); err == nil || !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("symlink remote target: got %v, want rejection", err)
	}
}

func TestDownloadRefusesExistingLocalDestination(t *testing.T) {
	local := filepath.Join(t.TempDir(), "existing.txt")
	if err := os.WriteFile(local, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	service := &serverService{}
	if err := service.Download("server", "/tmp/remote.txt", local, ports.TransferOptions{}); err == nil || !strings.Contains(err.Error(), "--overwrite") {
		t.Fatalf("existing local target: got %v, want overwrite error", err)
	}
}

func TestDownloadUsesLegacySCPAndRejectsSymlinkDestination(t *testing.T) {
	binDir := t.TempDir()
	sshScript := "#!/bin/sh\necho 'OpenSSH_9.9p1' >&2\n"
	scpScript := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$LAZYSSH_TEST_SCP_ARGS\"\n"
	if err := os.WriteFile(filepath.Join(binDir, "ssh"), []byte(sshScript), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "scp"), []byte(scpScript), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	argFile := filepath.Join(t.TempDir(), "args")
	t.Setenv("LAZYSSH_TEST_SCP_ARGS", argFile)
	local := filepath.Join(t.TempDir(), "downloaded file.txt")
	service := &serverService{
		serverRepository: &serverServiceTestRepo{servers: []domain.Server{{Alias: "server"}}},
		sshConfigPath:    "/runtime/config",
	}
	remote := "/tmp/it's a file.txt"
	if err := service.Download("server", remote, local, ports.TransferOptions{Legacy: true}); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(argFile)
	if err != nil {
		t.Fatal(err)
	}
	args := string(content)
	if !strings.Contains(args, "-O\n") || !strings.Contains(args, "server:"+quotePOSIXShell(remote)+"\n") || !strings.Contains(args, local+"\n") {
		t.Fatalf("legacy download args = %q", args)
	}
	if err := os.Symlink(argFile, local); err != nil {
		t.Fatal(err)
	}
	if err := service.Download("server", remote, local, ports.TransferOptions{Overwrite: true}); err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("symlink destination: got %v, want rejection", err)
	}
}

func TestDirectoryUploadRequiresRecursive(t *testing.T) {
	_, _, err := transferLocalSource(t.TempDir(), false)
	if err == nil || !strings.Contains(err.Error(), "--recursive") {
		t.Fatalf("directory source: got %v, want recursive error", err)
	}
}

func TestLegacyTransferModeForOldOpenSSH(t *testing.T) {
	binDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(binDir, "ssh"), []byte("#!/bin/sh\necho 'OpenSSH_8.6p1' >&2\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	legacy, forceFlag := legacyTransferMode(false)
	if !legacy || forceFlag {
		t.Fatalf("old OpenSSH mode = (%v, %v), want (true, false)", legacy, forceFlag)
	}
	legacy, forceFlag = legacyTransferMode(true)
	if !legacy || forceFlag {
		t.Fatalf("requested legacy on old OpenSSH = (%v, %v), want (true, false)", legacy, forceFlag)
	}
}
