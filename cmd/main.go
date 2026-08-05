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

package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"text/tabwriter"

	data_adapter "github.com/xiaocheng2014/lazyssh/internal/adapters/data"
	"github.com/xiaocheng2014/lazyssh/internal/adapters/data/ssh_config_file"
	"github.com/xiaocheng2014/lazyssh/internal/logger"
	"github.com/xiaocheng2014/lazyssh/internal/vault"

	"github.com/spf13/cobra"
	"github.com/xiaocheng2014/lazyssh/internal/adapters/ui"
	"github.com/xiaocheng2014/lazyssh/internal/core/domain"
	"github.com/xiaocheng2014/lazyssh/internal/core/ports"
	"github.com/xiaocheng2014/lazyssh/internal/core/services"
	"go.uber.org/zap"
	"golang.org/x/term"
)

var (
	version                     = "develop"
	gitCommit                   = "unknown"
	malformedLazySSHHostComment = regexp.MustCompile(`(?m)^([ \t]*Host(?:[ \t]+|[ \t]*=[ \t]*)[^#\r\n]*\S)(#Added by lazyssh)(\r?)$`)
)

func main() {
	if handled, err := services.HandleSSHAskpass(os.Args[1:], os.Stdout); handled {
		if err != nil {
			_, _ = fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if err := run(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	log, err := logger.New("LAZYSSH")
	if err != nil {
		return err
	}

	//nolint:errcheck // log.Sync may return an error which is safe to ignore here
	defer log.Sync()

	return newRootCommand(log).Execute()
}

func newRootCommand(log *zap.SugaredLogger) *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   ui.AppName,
		Short: "SSH 服务器管理与快捷连接工具",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			home, err := userHome()
			if err != nil {
				return err
			}
			return runTUI(log, home)
		},
	}
	rootCmd.SilenceUsage = true
	passwordCmd := &cobra.Command{
		Use:   "password",
		Short: "测试或修改加密仓库密码",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	passwordCmd.AddCommand(
		&cobra.Command{
			Use:   "test",
			Short: "测试密码是否能够解密配置仓库",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, args []string) error {
				home, err := userHome()
				if err != nil {
					return err
				}
				return runPasswordTest(home, cmd.OutOrStdout())
			},
		},
		&cobra.Command{
			Use:   "change",
			Short: "修改加密密码并更新本地密码文件",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, args []string) error {
				home, err := userHome()
				if err != nil {
					return err
				}
				return runPasswordChange(home, cmd.OutOrStdout())
			},
		},
	)
	rootCmd.AddCommand(
		&cobra.Command{
			Use:   "list",
			Short: "列出所有 SSH 服务器及其固定序号",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, args []string) error {
				home, err := userHome()
				if err != nil {
					return err
				}
				return runList(log, home, cmd.OutOrStdout())
			},
		},
		&cobra.Command{
			Use:   "go <index>",
			Short: "根据 lazyssh list 中的序号直接连接服务器",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				home, err := userHome()
				if err != nil {
					return err
				}
				return runGo(log, home, args[0])
			},
		},
		&cobra.Command{
			Use:   "edit",
			Short: "使用编辑器修改 SSH 服务器配置",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, args []string) error {
				home, err := userHome()
				if err != nil {
					return err
				}
				return runEdit(log, home, cmd.OutOrStdout())
			},
		},
		passwordCmd,
	)
	return rootCmd
}

func userHome() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get user home directory: %w", err)
	}
	return home, nil
}

type runtimeServices struct {
	manager       *vault.Manager
	serverService ports.ServerService
	keyService    ports.KeyService
	sshConfigPath string
}

func openRuntime(log *zap.SugaredLogger, home string) (*runtimeServices, error) {
	manager, err := openVault(home)
	if err != nil {
		return nil, err
	}
	sshConfigFile := manager.Path(vault.ConfigName)
	repaired, err := repairLazySSHHostComments(sshConfigFile)
	if err != nil {
		_ = manager.CloseReadOnly()
		return nil, err
	}
	if repaired {
		if err := manager.Save(); err != nil {
			_ = manager.CloseReadOnly()
			return nil, fmt.Errorf("save repaired SSH host comments: %w", err)
		}
	}
	metaDataFile := manager.Path(vault.MetadataName)
	baseRepo := ssh_config_file.NewRepository(log, sshConfigFile, metaDataFile)
	connectionAliasesChanged, err := baseRepo.EnsureConnectionAliases()
	if err != nil {
		_ = manager.CloseReadOnly()
		return nil, err
	}
	if connectionAliasesChanged {
		if err := manager.Save(); err != nil {
			_ = manager.CloseReadOnly()
			return nil, fmt.Errorf("save internal SSH connection aliases: %w", err)
		}
	}
	serverRepo := data_adapter.NewSyncingRepository(baseRepo, manager.Save)
	keyService := services.NewKeyService(manager.Path(vault.KeysDirName), manager.Path("keys.json"), manager.Save)
	serverService := services.NewServerService(log, serverRepo, keyService, sshConfigFile)
	return &runtimeServices{manager: manager, serverService: serverService, keyService: keyService, sshConfigPath: sshConfigFile}, nil
}

func repairLazySSHHostComments(configPath string) (bool, error) {
	content, err := os.ReadFile(configPath)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read SSH config for comment migration: %w", err)
	}
	repaired := malformedLazySSHHostComment.ReplaceAll(content, []byte("$1    $2$3"))
	if bytes.Equal(content, repaired) {
		return false, nil
	}

	temp, err := os.CreateTemp(filepath.Dir(configPath), ".config-comment-repair-*.tmp")
	if err != nil {
		return false, fmt.Errorf("create SSH config migration file: %w", err)
	}
	tempPath := temp.Name()
	defer func() { _ = os.Remove(tempPath) }()
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return false, fmt.Errorf("secure SSH config migration file: %w", err)
	}
	if _, err := temp.Write(repaired); err != nil {
		_ = temp.Close()
		return false, fmt.Errorf("write SSH config migration file: %w", err)
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return false, fmt.Errorf("sync SSH config migration file: %w", err)
	}
	if err := temp.Close(); err != nil {
		return false, fmt.Errorf("close SSH config migration file: %w", err)
	}
	if err := os.Rename(tempPath, configPath); err != nil {
		return false, fmt.Errorf("replace SSH config after comment migration: %w", err)
	}
	return true, nil
}

func openVault(home string) (*vault.Manager, error) {
	vaultDir := filepath.Join(home, ".config", "lazyssh")
	if err := os.MkdirAll(vaultDir, 0o700); err != nil {
		return nil, fmt.Errorf("create lazyssh vault directory: %w", err)
	}
	if err := writeGitSupportFiles(vaultDir); err != nil {
		return nil, err
	}

	var manager *vault.Manager
	var err error
	if vault.Exists(vaultDir) {
		manager, err = unlockVault(vaultDir)
	} else {
		fmt.Fprintln(os.Stderr, "No encrypted LazySSH vault was found. Creating one now.")
		password, promptErr := createVaultPassword()
		if promptErr != nil {
			return nil, promptErr
		}
		defer clearBytes(password)
		initialFiles := map[string]string{}
		if !fileExists(filepath.Join(vaultDir, vault.ConfigName)) {
			initialFiles[vault.ConfigName] = filepath.Join(home, ".ssh", "config")
		}
		if !fileExists(filepath.Join(vaultDir, vault.MetadataName)) {
			initialFiles[vault.MetadataName] = filepath.Join(home, ".lazyssh", "metadata.json")
		}
		manager, err = vault.Create(vaultDir, password, initialFiles)
		if err == nil {
			if sealErr := manager.SealPlaintext(); sealErr != nil {
				_ = manager.Close()
				err = sealErr
			} else if passwordErr := manager.SaveLocalPassword(); passwordErr != nil {
				_ = manager.Close()
				err = passwordErr
			}
		}
	}
	if err != nil {
		return nil, err
	}
	return manager, nil
}

func runTUI(log *zap.SugaredLogger, home string) error {
	runtime, err := openRuntime(log, home)
	if err != nil {
		return err
	}
	application := ui.NewTUI(log, runtime.serverService, runtime.keyService, runtime.manager, version, gitCommit)

	runErr := application.Run()
	closeErr := runtime.manager.CloseReadOnly()
	return errors.Join(runErr, closeErr)
}

func runList(log *zap.SugaredLogger, home string, output io.Writer) error {
	runtime, err := openRuntime(log, home)
	if err != nil {
		return err
	}
	servers, listErr := runtime.serverService.ListServers("")
	if listErr == nil {
		listErr = writeServerList(output, servers)
	}
	return errors.Join(listErr, runtime.manager.CloseReadOnly())
}

func runGo(log *zap.SugaredLogger, home, indexValue string) error {
	runtime, err := openRuntime(log, home)
	if err != nil {
		return err
	}
	servers, operationErr := runtime.serverService.ListServers("")
	if operationErr == nil {
		var server domain.Server
		server, operationErr = serverAtIndex(servers, indexValue)
		if operationErr == nil {
			operationErr = runtime.serverService.SSH(server.Alias)
		}
	}
	return errors.Join(operationErr, runtime.manager.CloseReadOnly())
}

func runEdit(log *zap.SugaredLogger, home string, output io.Writer) error {
	runtimeServices, err := openRuntime(log, home)
	if err != nil {
		return err
	}
	before, operationErr := os.ReadFile(runtimeServices.sshConfigPath)
	if os.IsNotExist(operationErr) {
		before = nil
		operationErr = nil
	}
	if operationErr == nil {
		operationErr = launchEditor(runtimeServices.sshConfigPath)
	}
	if operationErr == nil {
		var after []byte
		after, operationErr = os.ReadFile(runtimeServices.sshConfigPath)
		if operationErr == nil && !bytes.Equal(before, after) {
			operationErr = prepareEditedSSHConfig(log, runtimeServices.sshConfigPath, runtimeServices.manager.Path(vault.MetadataName))
			if operationErr == nil {
				operationErr = runtimeServices.manager.Save()
				if operationErr == nil {
					_, operationErr = fmt.Fprintln(output, "SSH 服务器配置已保存到加密仓库。")
				}
			}
		} else if operationErr == nil {
			_, operationErr = fmt.Fprintln(output, "SSH 服务器配置没有变化。")
		}
	}
	return errors.Join(operationErr, runtimeServices.manager.CloseReadOnly())
}

func prepareEditedSSHConfig(log *zap.SugaredLogger, configPath, metadataPath string) error {
	if err := validateSSHConfig(configPath); err != nil {
		return err
	}
	repository := ssh_config_file.NewRepository(log, configPath, metadataPath)
	changed, err := repository.EnsureConnectionAliases()
	if err != nil {
		return err
	}
	if changed {
		return validateSSHConfig(configPath)
	}
	return nil
}

func launchEditor(configPath string) error {
	editorSpec := strings.TrimSpace(os.Getenv("VISUAL"))
	if editorSpec == "" {
		editorSpec = strings.TrimSpace(os.Getenv("EDITOR"))
	}
	if editorSpec == "" {
		if runtime.GOOS == "windows" {
			editorSpec = "notepad"
		} else {
			editorSpec = "vi"
		}
	}
	parts := strings.Fields(editorSpec)
	if len(parts) == 0 {
		return errors.New("编辑器命令不能为空")
	}
	args := append(append([]string(nil), parts[1:]...), configPath)
	// #nosec G204 -- VISUAL/EDITOR is an explicit user-controlled local command.
	command := exec.Command(parts[0], args...)
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("运行编辑器 %q：%w", editorSpec, err)
	}
	return nil
}

func validateSSHConfig(configPath string) error {
	command := exec.Command("ssh", "-G", "-F", configPath, "lazyssh-config-validation")
	output, err := command.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			message = err.Error()
		}
		return fmt.Errorf("SSH 配置格式错误，修改未保存：%s", message)
	}
	return nil
}

func runPasswordTest(home string, output io.Writer) error {
	manager, err := openVault(home)
	if err != nil {
		return err
	}
	password, operationErr := readPassword("请输入要测试的加密密码：")
	if operationErr == nil {
		defer clearBytes(password)
		operationErr = manager.VerifyPassword(password)
		if operationErr == nil {
			_, operationErr = fmt.Fprintln(output, "密码正确，可以正常解密配置仓库。")
		}
	}
	return errors.Join(operationErr, manager.CloseReadOnly())
}

func runPasswordChange(home string, output io.Writer) error {
	manager, err := openVault(home)
	if err != nil {
		return err
	}
	password, operationErr := readConfirmedPassword("请输入新密码（至少 10 个字符）：", "请再次输入新密码：")
	if operationErr == nil {
		defer clearBytes(password)
		operationErr = manager.ChangePassword(password)
		if operationErr == nil {
			_, operationErr = fmt.Fprintln(output, "加密密码和本地密码文件已更新。")
		}
	}
	return errors.Join(operationErr, manager.CloseReadOnly())
}

func writeServerList(output io.Writer, servers []domain.Server) error {
	writer := tabwriter.NewWriter(output, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(writer, "NO.\tALIAS\tTARGET"); err != nil {
		return err
	}
	for index, server := range servers {
		if _, err := fmt.Fprintf(writer, "%d\t%s\t%s\n", index+1, server.Alias, serverTarget(server)); err != nil {
			return err
		}
	}
	return writer.Flush()
}

func serverTarget(server domain.Server) string {
	host := server.Host
	if host == "" {
		host = server.Alias
	}
	port := server.Port
	if port == 0 {
		port = 22
	}
	target := net.JoinHostPort(host, strconv.Itoa(port))
	if server.User != "" {
		target = server.User + "@" + target
	}
	return target
}

func serverAtIndex(servers []domain.Server, indexValue string) (domain.Server, error) {
	index, err := strconv.Atoi(strings.TrimSpace(indexValue))
	if err != nil || index < 1 {
		return domain.Server{}, fmt.Errorf("invalid server index %q: use a positive number shown by 'lazyssh list'", indexValue)
	}
	if index > len(servers) {
		return domain.Server{}, fmt.Errorf("server index %d is out of range; available indexes are 1-%d", index, len(servers))
	}
	return servers[index-1], nil
}

func unlockVault(vaultDir string) (*vault.Manager, error) {
	password, found, err := vault.LoadLocalPassword(vaultDir)
	if err != nil {
		return nil, err
	}
	if found {
		defer clearBytes(password)
		manager, unlockErr := vault.Unlock(vaultDir, password)
		if unlockErr != nil {
			return nil, fmt.Errorf("unlock with locally stored password %s: %w", vault.LocalPasswordPath(vaultDir), unlockErr)
		}
		return manager, nil
	}

	password, err = readPassword("LazySSH vault password (saved on this device after unlock): ")
	if err != nil {
		return nil, err
	}
	defer clearBytes(password)
	manager, err := vault.Unlock(vaultDir, password)
	if err != nil {
		return nil, err
	}
	if err := manager.SaveLocalPassword(); err != nil {
		_ = manager.Close()
		return nil, err
	}
	return manager, nil
}

func readPassword(prompt string) ([]byte, error) {
	_, _ = fmt.Fprint(os.Stderr, prompt)
	password, err := term.ReadPassword(int(os.Stdin.Fd()))
	_, _ = fmt.Fprintln(os.Stderr)
	if err != nil {
		return nil, fmt.Errorf("read vault password: %w", err)
	}
	return password, nil
}

func createVaultPassword() ([]byte, error) {
	return readConfirmedPassword("请创建加密密码（至少 10 个字符）：", "请再次输入加密密码：")
}

func readConfirmedPassword(passwordPrompt, confirmationPrompt string) ([]byte, error) {
	password, err := readPassword(passwordPrompt)
	if err != nil {
		return nil, err
	}
	if len(password) < 10 || strings.TrimSpace(string(password)) == "" {
		clearBytes(password)
		return nil, errors.New("加密密码至少需要 10 个字符")
	}
	confirmation, err := readPassword(confirmationPrompt)
	if err != nil {
		clearBytes(password)
		return nil, err
	}
	defer clearBytes(confirmation)
	if !bytes.Equal(password, confirmation) {
		clearBytes(password)
		return nil, errors.New("两次输入的加密密码不一致")
	}
	return password, nil
}

func writeGitSupportFiles(vaultDir string) error {
	gitignore := filepath.Join(vaultDir, ".gitignore")
	content, err := os.ReadFile(gitignore)
	if os.IsNotExist(err) {
		content = []byte("*\n!.gitignore\n!.gitattributes\n!" + vault.BundleName + "\n")
	} else if err != nil {
		return fmt.Errorf("read vault .gitignore: %w", err)
	}
	if !containsGitignoreRule(content, vault.LocalPasswordName+"*") {
		if len(content) > 0 && content[len(content)-1] != '\n' {
			content = append(content, '\n')
		}
		content = append(content, []byte(vault.LocalPasswordName+"*\n")...)
	}
	if err := os.WriteFile(gitignore, content, 0o600); err != nil {
		return fmt.Errorf("write vault .gitignore: %w", err)
	}
	gitattributes := filepath.Join(vaultDir, ".gitattributes")
	if !fileExists(gitattributes) {
		if err := os.WriteFile(gitattributes, []byte(vault.BundleName+" binary\n"), 0o600); err != nil {
			return fmt.Errorf("write vault .gitattributes: %w", err)
		}
	}
	return nil
}

func containsGitignoreRule(content []byte, rule string) bool {
	for _, line := range strings.Split(string(content), "\n") {
		if strings.TrimSpace(line) == rule {
			return true
		}
	}
	return false
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func clearBytes(value []byte) {
	for i := range value {
		value[i] = 0
	}
}
