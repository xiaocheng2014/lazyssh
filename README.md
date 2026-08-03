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

## Non-interactive commands

List every server using stable, one-based indexes:

```bash
lazyssh list
```

Connect directly using an index from that list without opening the TUI:

```bash
lazyssh go 3
```

Both commands use the encrypted LazySSH vault, dedicated SSH config, and any managed key bound to the selected server. `lazyssh list` is read-only and does not rewrite the encrypted bundle.

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


## 个人新增功能

- T ：macOS用于本地wireshark，实时显示远程 tcpdump数据
