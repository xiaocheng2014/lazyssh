package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xiaocheng2014/lazyssh/internal/core/domain"
	"github.com/xiaocheng2014/lazyssh/internal/vault"
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

func TestRootCommandIncludesListAndGo(t *testing.T) {
	root := newRootCommand(nil)
	for _, name := range []string{"list", "go"} {
		command, _, err := root.Find([]string{name})
		if err != nil {
			t.Fatal(err)
		}
		if command.Name() != name {
			t.Fatalf("command %q resolved to %q", name, command.Name())
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
