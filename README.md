<div align="center">
  <img src="./docs/logo.png" alt="lazyssh logo" width="600" height="600"/>
</div>

---

Lazyssh is a terminal-based, interactive SSH manager inspired by tools like lazydocker and k9s — but built for managing your fleet of servers directly from your terminal.
<br/>
With lazyssh, you can quickly navigate, connect, manage SSH keys, and work with servers stored in a password-encrypted portable vault. No more remembering IP addresses or maintaining device-specific key paths — just a clean, keyboard-driven UI.

---

## ✨ Features

### Server Management
- 📜 Read & display servers from the encrypted LazySSH vault in a scrollable list.
- ➕ Add a new server from the UI with comprehensive SSH configuration options.
- ✏ Edit existing server entries directly from the UI with a tabbed interface.
- 🗑 Delete server entries safely.
- 📌 Pin / unpin servers to keep favorites at the top.
- 🏓 Ping server to check status.

### Quick Server Navigation
- 🔍 Fuzzy search by alias, IP, or tags.
- 🖥 One‑keypress SSH into the selected server (Enter).
- 🏷 Tag servers (e.g., prod, dev, test) for quick filtering.
- ↕️ Sort by alias or last SSH (toggle + reverse).

### Advanced SSH Configuration
- 🔗 Port forwarding (LocalForward, RemoteForward, DynamicForward).
- 🚀 Connection multiplexing for faster subsequent connections.
- 🔐 Advanced authentication options (public key, password, agent forwarding).
- 🔒 Security settings (ciphers, MACs, key exchange algorithms).
- 🌐 Proxy settings (ProxyJump, ProxyCommand).
- ⚙️ Extensive SSH config options organized in tabbed interface.

### Key Management
- 🔑 Import existing OpenSSH private or public keys.
- 🔎 Validate keys and display their type and SHA-256 fingerprint.
- 🔗 Bind a managed private key to a server without storing device-specific paths.
- 📋 Copy public keys and restore private/public keys to an explicit path.
- 🗑 Delete unused keys after checking server bindings.

### Password-encrypted portable vault
- 🔐 Encrypt the SSH config, metadata, public keys, and private keys into one `lazyssh.bundle.age` file.
- 🔓 Decrypt only to a permission-restricted temporary directory while LazySSH is running.
- 💻 Save the password in an owner-only local file so normal starts unlock automatically.
- 💾 Re-encrypt immediately after configuration, metadata, or key changes.
- 🔄 Clone the vault Git repository on another device and unlock it with the same password.
- 🔑 Change the vault password from the TUI.


### Upcoming
- 📁 Copy files between local and servers with an easy picker UI.
- 🔑 SSH Key Deployment Features:
    - Use default local public key (`~/.ssh/id_ed25519.pub` or `~/.ssh/id_rsa.pub`)
    - Paste custom public keys manually
    - Generate new keypairs and deploy them
    - Automatically append keys to `~/.ssh/authorized_keys` with correct permissions
---

## 🚀 新增功能说明（custom_main）

### 独立配置与自动迁移

- LazySSH 不再直接修改 `~/.ssh/config`，而是使用 `~/.config/lazyssh` 作为独立配置目录。
- 首次启动会导入现有的 `~/.ssh/config` 和 `~/.lazyssh/metadata.json`，原文件保持不变。
- 所有 SSH 连接通过系统 OpenSSH 执行，并使用 `ssh -F <LazySSH 临时配置> <Host 别名>`，因此 ProxyJump、端口转发和其他 OpenSSH 配置仍然有效。
- 已自动修复旧版本生成的 `Host alias#Added by lazyssh` 格式，避免数字别名被 OpenSSH 当作旧式 IPv4 简写解析。

### 密码加密与 Git 迁移

- 配置、元数据以及托管的公私钥会整体加密到 `~/.config/lazyssh/lazyssh.bundle.age`。
- 本机密码保存在 `~/.config/lazyssh/.vault-password`，权限为 `0600`；启动时自动读取，无需每次输入。
- `.vault-password`、运行锁和明文临时目录不会进入 Git，Git 只需要管理加密包及辅助配置。
- 新设备克隆保险库后首次输入密码，验证成功后会生成该设备自己的本地密码文件。
- 程序异常退出产生的失效锁会根据 PID 自动识别并清理，仍在运行的实例不会被误解锁。

### 公私钥管理

在服务器列表按 `K` 打开密钥管理界面：

| 按键 | 功能 |
| --- | --- |
| `i` | 导入 OpenSSH 私钥并自动生成对应公钥信息 |
| `o` | 导入公钥 |
| `b` | 将选中的托管私钥绑定到当前服务器 |
| `x` | 解除当前服务器的密钥绑定 |
| `c` | 复制公钥 |
| `e` | 将公钥或公私钥还原到指定路径 |
| `d` | 删除未被服务器使用的托管密钥 |

LazySSH 会校验密钥格式并显示类型及 SHA-256 指纹。连接绑定服务器时，会自动向 OpenSSH 传入 `IdentitiesOnly=yes` 和对应的临时私钥路径。

### 非交互命令

无需进入 TUI 即可列出服务器：

```bash
lazyssh list
```

示例输出：

```text
NO.  ALIAS     TARGET
1    server-a  root@192.0.2.10:22
2    server-b  admin@192.0.2.20:2202
```

使用相同序号直接连接：

```bash
lazyssh go 2
```

`list` 和 `go` 使用完全相同的稳定排序。`go` 继续使用专用 SSH 配置、服务器绑定的托管私钥以及连接元数据记录；`list` 是只读操作，不会无故改写加密包。

### 远程抓包

在 macOS 中按 `T` 可以通过 SSH 在远端执行 `tcpdump`，并将数据流交给本机 Wireshark 实时显示。可使用 `WIRESHARK_PATH` 环境变量指定 Wireshark 可执行文件。

---

## 🔐 Security Notice

LazySSH stores its portable state in `~/.config/lazyssh/lazyssh.bundle.age` using password-based age encryption.

- All SSH connections are executed through your system’s native ssh binary (OpenSSH).

- Imported private keys are copied into the encrypted vault. Plaintext keys exist only in a `0700` temporary runtime directory while the vault is unlocked.

- The vault password is stored locally in `~/.config/lazyssh/.vault-password` with owner-only permissions. It is excluded from both the encrypted bundle and Git, and is used automatically for encryption and decryption on that device.

- On a new device, LazySSH asks for the password once after cloning the encrypted bundle, verifies it, and then stores it locally. Losing both the password and every device-local password file makes the encrypted bundle unrecoverable.

- Your existing IdentityFile paths and ssh-agent integrations work exactly as before.

- LazySSH does not modify or delete `~/.ssh/config` or the original key files selected during import.

- On first use, LazySSH imports the existing `~/.ssh/config` and metadata into the encrypted bundle without changing or deleting the original files.

- Temporary configuration, metadata, and managed key files use owner-only permissions.


## 🛡️ Config Safety: Non‑destructive writes and backups

- Non‑destructive edits: LazySSH only writes the minimal required SSH config changes. Its parser preserves existing comments, spacing, order, and settings it did not touch.
- Atomic writes: updates are written to a temporary file and then atomically renamed over the original, minimizing the risk of partial writes.
- Encrypted persistence: a newly encrypted bundle is verified before atomically replacing the previous bundle.
- Backups:
  - SSH config backups are retained inside the encrypted bundle.
  - Git can provide version history for the complete encrypted bundle.

## Git backup and migration

The vault directory contains `.gitignore` and `.gitattributes` files configured so Git includes the encrypted bundle but excludes plaintext runtime data.

```bash
cd ~/.config/lazyssh
git init
git add .gitignore .gitattributes lazyssh.bundle.age
git commit -m "backup lazyssh vault"
git remote add origin <your-private-or-public-repository>
git push -u origin main
```

On another device, clone the repository to `~/.config/lazyssh` and run `lazyssh`. Enter the same vault password once to restore the SSH configuration, metadata, and managed keys; later starts use that device's local password file automatically. Because the encrypted bundle is an opaque binary file, pull before editing and avoid changing it concurrently on multiple devices.

## 📷 Screenshots

<div align="center">

### 🚀 Startup
<img src="./docs/loader.png" alt="App starting splash/loader" width="800" />

Clean loading screen when launching the app

---

### 📋 Server Management Dashboard
<img src="./docs/list server.png" alt="Server list view" width="900" />

Main dashboard displaying all configured servers with status indicators, pinned favorites at the top, and easy navigation

---

### 🔎 Search
<img src="./docs/search.png" alt="Fuzzy search servers" width="900" />

Fuzzy search functionality to quickly find servers by name, IP address, or tags

---

### ➕ Add/Edit Server
<img src="./docs/add server.png" alt="Add a new server" width="900" />

Tabbed interface for managing SSH connections with extensive configuration options organized into:
- **Basic** - Host, user, port, keys, tags
- **Connection** - Proxy, timeouts, multiplexing, canonicalization
- **Forwarding** - Port forwarding, X11, agent
- **Authentication** - Keys, passwords, methods, algorithm settings
- **Advanced** - Security, cryptography, environment, debugging

---

### 🔐 Connect to server
<img src="./docs/ssh.png" alt="SSH connection details" width="900" />

SSH into the selected server

</div>

---

## 📦 Installation

### Option 1: Download Binary from Releases

Download from [GitHub Releases](https://github.com/xiaocheng2014/lazyssh/releases). You can use the snippet below to automatically fetch the latest version for your OS/ARCH (Darwin/Linux and amd64/arm64 supported):

```bash
# Detect latest version
LATEST_TAG=$(curl -fsSL https://api.github.com/repos/xiaocheng2014/lazyssh/releases/latest | jq -r .tag_name)
# Download the correct binary for your system
curl -LJO "https://github.com/xiaocheng2014/lazyssh/releases/download/${LATEST_TAG}/lazyssh_$(uname)_$(uname -m).tar.gz"
# Extract the binary
tar -xzf lazyssh_$(uname)_$(uname -m).tar.gz
# Move to /usr/local/bin or another directory in your PATH
sudo mv lazyssh /usr/local/bin/
# enjoy!
lazyssh
```

### Option 2: Build from Source

```bash
# Clone the repository
git clone https://github.com/xiaocheng2014/lazyssh.git
cd lazyssh

# Build for macOS
make build
./bin/lazyssh

# Or Run it directly
make run
```

---

## ⌨️ Key Bindings

| Key   | Action                        |
| ----- | ----------------------------- |
| /     | Toggle search bar             |
| ↑↓/jk | Navigate servers              |
| Enter | SSH into selected server      |
| K     | Manage/import/restore keys    |
| V     | View vault/change password    |
| c     | Copy SSH command to clipboard |
| g     | Ping selected server          |
| r     | Refresh background data       |
| a     | Add server                    |
| e     | Edit server                   |
| t     | Edit tags                     |
| d     | Delete server                 |
| p     | Pin/Unpin server              |
| s     | Toggle sort field             |
| S     | Reverse sort order            |
| q     | Quit                          |

**In Server Form:**
| Key    | Action               |
| ------ | -------------------- |
| Ctrl+H | Previous tab         |
| Ctrl+L | Next tab             |
| Ctrl+S | Save                 |
| Esc    | Cancel               |

Tip: The hint bar at the top of the list shows the most useful shortcuts.

---

## 🤝 Contributing

Contributions are welcome!

- If you spot a bug or have a feature request, please [open an issue](https://github.com/xiaocheng2014/lazyssh/issues).
- If you'd like to contribute, fork the repo and submit a pull request ❤️.

We love seeing the community make Lazyssh better 🚀

### Semantic Pull Requests

This repository enforces semantic PR titles via an automated GitHub Action. Please format your PR title as:

- type(scope): short descriptive subject
Notes:
- Scope is optional and should be one of: ui, cli, config, parser.

Allowed types in this repo:
- feat: a new feature
- fix: a bug fix
- improve: quality or UX improvements that are not a refactor or perf
- refactor: code change that neither fixes a bug nor adds a feature
- docs: documentation only changes
- test: adding or refactoring tests
- ci: CI/CD or automation changes
- chore: maintenance tasks, dependency bumps, non-code infra
- revert: reverts a previous commit

Examples:
- feat(ui): add server pinning and sorting options
- fix(parser): handle comments at end of Host blocks
- improve(cli): show friendly error when ssh binary missing
- refactor(config): simplify backup rotation logic
- ci: cache Go toolchain and dependencies

Tip: If your PR touches multiple areas, pick the most relevant scope or omit the scope.

---

## ⭐ Support

If you find Lazyssh useful, please consider giving the repo a **star** ⭐️ and join [stargazers](https://github.com/xiaocheng2014/lazyssh/stargazers).

☕ You can also support me by [buying me a coffee](https://www.buymeacoffee.com/xiaocheng2014) ❤️
<br/>
<a href="https://buymeacoffee.com/xiaocheng2014" target="_blank"><img src="https://cdn.buymeacoffee.com/buttons/v2/default-yellow.png" width="200"></a>


---

## 🙏 Acknowledgments

- Built with [tview](https://github.com/rivo/tview) and [tcell](https://github.com/gdamore/tcell).
- Inspired by [k9s](https://github.com/derailed/k9s) and [lazydocker](https://github.com/jesseduffield/lazydocker).
