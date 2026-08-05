// Copyright 2025.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package ssh_config_file

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kevinburke/ssh_config"
	"github.com/xiaocheng2014/lazyssh/internal/core/domain"
	"go.uber.org/zap"
)

func TestCreateHostSeparatesLazySSHComment(t *testing.T) {
	repository := &Repository{}
	host := repository.createHostFromServer(domain.Server{Alias: "example", Host: "192.0.2.1"})
	if firstLine := strings.SplitN(host.String(), "\n", 2)[0]; firstLine != "Host example    #Added by lazyssh" {
		t.Fatalf("host line = %q", firstLine)
	}
}

func TestChineseAliasUsesHiddenOpenSSHConnectionAlias(t *testing.T) {
	repository := &Repository{}
	host := repository.createHostFromServer(domain.Server{Alias: "生产数据库", Host: "192.0.2.1"})
	connectionAlias := domain.SSHConnectionAlias("生产数据库")
	if !strings.HasPrefix(host.String(), "Host 生产数据库 "+connectionAlias) {
		t.Fatalf("host does not contain internal connection alias:\n%s", host.String())
	}

	config, err := ssh_config.Decode(strings.NewReader(host.String()))
	if err != nil {
		t.Fatal(err)
	}
	servers := repository.toDomainServer(config)
	if len(servers) != 1 || len(servers[0].Aliases) != 1 || servers[0].Alias != "生产数据库" {
		t.Fatalf("internal alias leaked into server list: %#v", servers)
	}
}

func TestEnsureConnectionAliasesMigratesChineseConfigForOpenSSH(t *testing.T) {
	sshPath, err := exec.LookPath("ssh")
	if err != nil {
		t.Skip("system ssh is unavailable")
	}
	directory := t.TempDir()
	configPath := filepath.Join(directory, "config")
	metadataPath := filepath.Join(directory, "metadata.json")
	if err := os.WriteFile(configPath, []byte("Host 生产数据库\n    HostName 192.0.2.1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	repository := NewRepository(zap.NewNop().Sugar(), configPath, metadataPath)
	changed, err := repository.EnsureConnectionAliases()
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("Chinese config was not migrated")
	}
	connectionAlias := domain.SSHConnectionAlias("生产数据库")
	command := exec.Command(sshPath, "-G", "-F", configPath, connectionAlias)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("OpenSSH rejected internal alias: %v\n%s", err, output)
	}
	if !strings.Contains(strings.ToLower(string(output)), "hostname 192.0.2.1") {
		t.Fatalf("OpenSSH did not resolve configured HostName:\n%s", output)
	}
	changed, err = repository.EnsureConnectionAliases()
	if err != nil || changed {
		t.Fatalf("second migration changed config again: changed=%v err=%v", changed, err)
	}
}

func TestLoginPasswordRoundTripsThroughSSHConfigComment(t *testing.T) {
	repository := &Repository{}
	wantPassword := "server password # with symbols!"
	host := repository.createHostFromServer(domain.Server{
		Alias:         "生产数据库",
		Host:          "192.0.2.1",
		LoginPassword: wantPassword,
	})
	configText := host.String()
	if !strings.Contains(configText, "PasswordAuthentication yes") {
		t.Fatalf("password authentication was not enabled:\n%s", configText)
	}
	if !strings.Contains(configText, "# LazySSH-Password: "+wantPassword) {
		t.Fatalf("saved password comment is missing:\n%s", configText)
	}

	config, err := ssh_config.Decode(strings.NewReader(configText))
	if err != nil {
		t.Fatal(err)
	}
	servers := repository.toDomainServer(config)
	if len(servers) != 1 || servers[0].LoginPassword != wantPassword {
		t.Fatalf("password round trip failed: %#v", servers)
	}
}

func TestLoginPasswordCanBeReadFromStandaloneEditComment(t *testing.T) {
	config, err := ssh_config.Decode(strings.NewReader("Host edited\n    HostName 192.0.2.1\n    # LazySSH-Password: edited password\n"))
	if err != nil {
		t.Fatal(err)
	}
	servers := (&Repository{}).toDomainServer(config)
	if len(servers) != 1 || servers[0].LoginPassword != "edited password" {
		t.Fatalf("standalone password comment was not loaded: %#v", servers)
	}
}

func TestClearingLoginPasswordRemovesOnlyLazySSHComment(t *testing.T) {
	repository := &Repository{}
	host := repository.createHostFromServer(domain.Server{
		Alias:         "example",
		Host:          "192.0.2.1",
		LoginPassword: "old-password",
	})
	for _, node := range host.Nodes {
		if kv, ok := node.(*ssh_config.KV); ok && strings.EqualFold(kv.Key, "PasswordAuthentication") {
			kv.Comment = "keep this | " + kv.Comment
		}
	}
	repository.updateHostNodes(host, domain.Server{Alias: "example", Host: "192.0.2.1", PasswordAuthentication: "yes"})
	configText := host.String()
	if strings.Contains(configText, passwordCommentKey) {
		t.Fatalf("password marker was not removed:\n%s", configText)
	}
	if !strings.Contains(configText, "#keep this") {
		t.Fatalf("unrelated comment was removed:\n%s", configText)
	}
}

func TestConvertCLIForwardToConfigFormat(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "basic local forward",
			input:    "8080:localhost:80",
			expected: "8080 localhost:80",
		},
		{
			name:     "local forward with bind address",
			input:    "127.0.0.1:8080:localhost:80",
			expected: "127.0.0.1:8080 localhost:80",
		},
		{
			name:     "local forward with wildcard bind",
			input:    "*:8080:localhost:80",
			expected: "*:8080 localhost:80",
		},
		{
			name:     "remote forward",
			input:    "8080:localhost:3000",
			expected: "8080 localhost:3000",
		},
		{
			name:     "remote forward with bind address",
			input:    "0.0.0.0:80:localhost:8080",
			expected: "0.0.0.0:80 localhost:8080",
		},
		{
			name:     "forward with IPv6 address",
			input:    "8080:[2001:db8::1]:80",
			expected: "8080 [2001:db8::1]:80",
		},
		{
			name:     "forward with domain",
			input:    "3306:db.example.com:3306",
			expected: "3306 db.example.com:3306",
		},
		{
			name:     "invalid format - only one colon",
			input:    "8080:localhost",
			expected: "8080:localhost", // returned as-is
		},
		{
			name:     "invalid format - no colons",
			input:    "8080",
			expected: "8080", // returned as-is
		},
	}

	r := &Repository{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := r.convertCLIForwardToConfigFormat(tt.input)
			if result != tt.expected {
				t.Errorf("convertCLIForwardToConfigFormat(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestConvertConfigForwardToCLIFormat(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "basic local forward",
			input:    "8080 localhost:80",
			expected: "8080:localhost:80",
		},
		{
			name:     "local forward with bind address",
			input:    "127.0.0.1:8080 localhost:80",
			expected: "127.0.0.1:8080:localhost:80",
		},
		{
			name:     "local forward with wildcard bind",
			input:    "*:8080 localhost:80",
			expected: "*:8080:localhost:80",
		},
		{
			name:     "remote forward",
			input:    "8080 localhost:3000",
			expected: "8080:localhost:3000",
		},
		{
			name:     "remote forward with bind address",
			input:    "0.0.0.0:80 localhost:8080",
			expected: "0.0.0.0:80:localhost:8080",
		},
		{
			name:     "forward with IPv6 address",
			input:    "8080 [2001:db8::1]:80",
			expected: "8080:[2001:db8::1]:80",
		},
		{
			name:     "forward with domain",
			input:    "3306 db.example.com:3306",
			expected: "3306:db.example.com:3306",
		},
		{
			name:     "already in CLI format",
			input:    "8080:localhost:80",
			expected: "8080:localhost:80", // returned as-is
		},
		{
			name:     "no space separator",
			input:    "8080",
			expected: "8080", // returned as-is
		},
	}

	r := &Repository{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := r.convertConfigForwardToCLIFormat(tt.input)
			if result != tt.expected {
				t.Errorf("convertConfigForwardToCLIFormat(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}
