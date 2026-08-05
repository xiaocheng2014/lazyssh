package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xiaocheng2014/lazyssh/internal/core/domain"
	"github.com/xiaocheng2014/lazyssh/internal/vault"
	"go.uber.org/zap"
)

func TestWriteGitSupportFilesAlwaysIgnoresLocalPassword(t *testing.T) {
	vaultDir := t.TempDir()
	gitignore := filepath.Join(vaultDir, ".gitignore")
	if err := os.WriteFile(gitignore, []byte("custom-file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeGitSupportFiles(vaultDir); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(gitignore)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), vault.LocalPasswordName+"*\n") {
		t.Fatalf("local password ignore rule is missing: %q", content)
	}
}

func TestWriteServerListUsesOneBasedStableIndexes(t *testing.T) {
	servers := []domain.Server{
		{Alias: "alpha", Host: "192.0.2.10", User: "root", Port: 2202},
		{Alias: "beta", Host: "2001:db8::1"},
	}
	var output bytes.Buffer
	if err := writeServerList(&output, servers); err != nil {
		t.Fatal(err)
	}
	want := "NO.  ALIAS  TARGET\n1    alpha  root@192.0.2.10:2202\n2    beta   [2001:db8::1]:22\n"
	if output.String() != want {
		t.Fatalf("server list:\n%s\nwant:\n%s", output.String(), want)
	}
}

func TestServerAtIndex(t *testing.T) {
	servers := []domain.Server{{Alias: "first"}, {Alias: "second"}}
	server, err := serverAtIndex(servers, "2")
	if err != nil {
		t.Fatal(err)
	}
	if server.Alias != "second" {
		t.Fatalf("selected alias = %q", server.Alias)
	}
	for _, value := range []string{"0", "3", "not-a-number"} {
		if _, err := serverAtIndex(servers, value); err == nil {
			t.Fatalf("serverAtIndex(%q) succeeded", value)
		}
	}
}

func TestRootCommandIncludesNonInteractiveCommands(t *testing.T) {
	root := newRootCommand(nil)
	for _, name := range []string{"list", "go", "edit", "password"} {
		command, _, err := root.Find([]string{name})
		if err != nil {
			t.Fatal(err)
		}
		if command.Name() != name {
			t.Fatalf("command %q resolved to %q", name, command.Name())
		}
	}
	for _, name := range []string{"test", "change"} {
		command, _, err := root.Find([]string{"password", name})
		if err != nil {
			t.Fatal(err)
		}
		if command.Name() != name {
			t.Fatalf("password command %q resolved to %q", name, command.Name())
		}
	}
}

func TestRepairLazySSHHostComments(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config")
	input := "Host broken#Added by lazyssh\n    HostName 192.0.2.1\nHost valid    #Added by lazyssh\nHost custom#keep this\n"
	if err := os.WriteFile(configPath, []byte(input), 0o600); err != nil {
		t.Fatal(err)
	}
	repaired, err := repairLazySSHHostComments(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !repaired {
		t.Fatal("malformed LazySSH comment was not repaired")
	}
	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	want := "Host broken    #Added by lazyssh\n    HostName 192.0.2.1\nHost valid    #Added by lazyssh\nHost custom#keep this\n"
	if string(content) != want {
		t.Fatalf("repaired config = %q, want %q", content, want)
	}
	repaired, err = repairLazySSHHostComments(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if repaired {
		t.Fatal("already repaired config changed again")
	}
}

func TestValidateSSHConfig(t *testing.T) {
	if _, err := exec.LookPath("ssh"); err != nil {
		t.Skip("ssh is unavailable")
	}
	configPath := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(configPath, []byte("Host 生产数据库\n    HostName 192.0.2.1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := validateSSHConfig(configPath); err != nil {
		t.Fatalf("valid SSH config rejected: %v", err)
	}
	if err := os.WriteFile(configPath, []byte("Host invalid\n    NotARealSSHOption yes\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := validateSSHConfig(configPath); err == nil {
		t.Fatal("invalid SSH config accepted")
	}
}

func TestPrepareEditedSSHConfigAddsInternalAliasForChineseHost(t *testing.T) {
	if _, err := exec.LookPath("ssh"); err != nil {
		t.Skip("ssh is unavailable")
	}
	directory := t.TempDir()
	configPath := filepath.Join(directory, "config")
	metadataPath := filepath.Join(directory, "metadata.json")
	if err := os.WriteFile(configPath, []byte("Host 新增中文服务器\n    HostName 192.0.2.10\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := prepareEditedSSHConfig(zap.NewNop().Sugar(), configPath, metadataPath); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	want := "Host 新增中文服务器 " + domain.SSHConnectionAlias("新增中文服务器")
	if !strings.Contains(string(content), want) {
		t.Fatalf("internal alias was not generated after edit:\n%s", content)
	}
}
