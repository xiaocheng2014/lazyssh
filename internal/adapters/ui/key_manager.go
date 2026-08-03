package ui

import (
	"fmt"
	"strings"

	"github.com/atotto/clipboard"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/xiaocheng2014/lazyssh/internal/core/domain"
)

func (t *tui) showKeyManager() {
	if t.keyService == nil {
		t.showStatusTempColor("Key management is unavailable", "#FF6B6B")
		return
	}
	keys, err := t.keyService.ListKeys()
	if err != nil {
		t.showKeyManagerError("Load keys", err)
		return
	}

	list := tview.NewList().ShowSecondaryText(true)
	list.SetBorder(true).
		SetTitle(" Managed SSH Keys ").
		SetTitleAlign(tview.AlignCenter)
	for _, key := range keys {
		kind := "public only"
		if key.HasPrivateKey() {
			kind = "private + public"
		}
		list.AddItem(fmt.Sprintf("%s  [%s]", key.Name, key.KeyType), fmt.Sprintf("%s  •  %s", key.Fingerprint, kind), 0, nil)
	}
	if len(keys) == 0 {
		list.AddItem("No managed keys", "Press i to import a private key or o to import a public key", 0, nil)
	}

	serverText := "No server selected"
	if server, ok := t.serverList.GetSelectedServer(); ok {
		serverText = "Selected server: [white]" + escapeForTview(server.Alias) + "[-]"
		if server.ManagedKeyID != "" {
			serverText += "  •  bound key: [white]" + escapeForTview(server.ManagedKeyID) + "[-]"
		}
	}
	help := tview.NewTextView().SetDynamicColors(true).
		SetText(serverText + "\n[white]i[-] Import private  •  [white]o[-] Import public  •  [white]b[-] Bind  •  [white]x[-] Unbind  •  [white]c[-] Copy public  •  [white]e[-] Restore  •  [white]d[-] Delete  •  [white]Esc/q[-] Back")
	help.SetBorder(true).SetTitle(" Actions ")

	view := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(list, 0, 1, true).
		AddItem(help, 4, 0, false)
	list.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Rune() {
		case 'i':
			t.showKeyImportForm(false)
			return nil
		case 'o':
			t.showKeyImportForm(true)
			return nil
		case 'b':
			if len(keys) == 0 {
				return nil
			}
			index := list.GetCurrentItem()
			if index >= 0 && index < len(keys) {
				t.bindManagedKey(keys[index])
			}
			return nil
		case 'x':
			t.unbindManagedKey()
			return nil
		case 'd':
			if len(keys) == 0 {
				return nil
			}
			index := list.GetCurrentItem()
			if index >= 0 && index < len(keys) {
				t.confirmDeleteManagedKey(keys[index])
			}
			return nil
		case 'c':
			if len(keys) > 0 {
				index := list.GetCurrentItem()
				if index >= 0 && index < len(keys) {
					t.copyManagedPublicKey(keys[index])
				}
			}
			return nil
		case 'e':
			if len(keys) > 0 {
				index := list.GetCurrentItem()
				if index >= 0 && index < len(keys) {
					t.showKeyExportForm(keys[index])
				}
			}
			return nil
		case 'q':
			t.returnToMain()
			return nil
		}
		if event.Key() == tcell.KeyEscape {
			t.returnToMain()
			return nil
		}
		return event
	})
	t.app.SetRoot(view, true)
	t.app.SetFocus(list)
}

func (t *tui) copyManagedPublicKey(key domain.ManagedKey) {
	publicKey, err := t.keyService.PublicKey(key.ID)
	if err != nil {
		t.showKeyManagerError("Copy public key", err)
		return
	}
	if err := clipboard.WriteAll(publicKey); err != nil {
		t.showKeyManagerError("Copy public key", err)
		return
	}
	t.showKeyManager()
}

func (t *tui) showKeyExportForm(key domain.ManagedKey) {
	form := tview.NewForm()
	form.SetBorder(true).SetTitle(" Restore Key: " + escapeForTview(key.Name) + " ").SetTitleAlign(tview.AlignCenter)
	form.AddInputField("Destination:", "", 100, nil, nil)
	form.AddTextView("", "The destination must not already exist. Private-key restores also create <destination>.pub.", 0, 2, true, false)
	form.AddButton("Restore", func() {
		destination := strings.TrimSpace(form.GetFormItem(0).(*tview.InputField).GetText())
		if err := t.keyService.ExportKey(key.ID, destination); err != nil {
			t.showKeyManagerError("Restore key", err)
			return
		}
		t.showKeyManager()
	})
	form.AddButton("Cancel", t.showKeyManager)
	form.SetCancelFunc(t.showKeyManager)
	form.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape {
			t.showKeyManager()
			return nil
		}
		return event
	})
	t.app.SetRoot(form, true)
	t.app.SetFocus(form)
}

func (t *tui) showKeyImportForm(publicOnly bool) {
	title := "Import Private Key"
	if publicOnly {
		title = "Import Public Key"
	}
	form := tview.NewForm()
	form.SetBorder(true).SetTitle(" " + title + " ").SetTitleAlign(tview.AlignCenter)
	form.AddInputField("Name:", "", 50, nil, nil)
	form.AddInputField("File path:", "", 100, nil, nil)
	form.AddButton("Import", func() {
		name := strings.TrimSpace(form.GetFormItem(0).(*tview.InputField).GetText())
		path := strings.TrimSpace(form.GetFormItem(1).(*tview.InputField).GetText())
		var importErr error
		t.app.Suspend(func() {
			if publicOnly {
				_, importErr = t.keyService.ImportPublicKey(name, path)
			} else {
				_, importErr = t.keyService.ImportPrivateKey(name, path)
			}
		})
		if importErr != nil {
			t.showKeyManagerError("Import key", importErr)
			return
		}
		t.showKeyManager()
	})
	form.AddButton("Cancel", t.showKeyManager)
	form.SetCancelFunc(t.showKeyManager)
	form.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape {
			t.showKeyManager()
			return nil
		}
		return event
	})
	t.app.SetRoot(form, true)
	t.app.SetFocus(form)
}

func (t *tui) bindManagedKey(key domain.ManagedKey) {
	server, ok := t.serverList.GetSelectedServer()
	if !ok {
		t.showKeyManagerError("Bind key", fmt.Errorf("no server selected"))
		return
	}
	if !key.HasPrivateKey() {
		t.showKeyManagerError("Bind key", fmt.Errorf("%q contains only a public key", key.Name))
		return
	}
	if err := t.serverService.SetManagedKey(server.Alias, key.ID); err != nil {
		t.showKeyManagerError("Bind key", err)
		return
	}
	t.refreshServerList()
	t.serverList.SelectAlias(server.Alias)
	t.showKeyManager()
}

func (t *tui) unbindManagedKey() {
	server, ok := t.serverList.GetSelectedServer()
	if !ok {
		t.showKeyManagerError("Unbind key", fmt.Errorf("no server selected"))
		return
	}
	if err := t.serverService.SetManagedKey(server.Alias, ""); err != nil {
		t.showKeyManagerError("Unbind key", err)
		return
	}
	t.refreshServerList()
	t.serverList.SelectAlias(server.Alias)
	t.showKeyManager()
}

func (t *tui) confirmDeleteManagedKey(key domain.ManagedKey) {
	servers, err := t.serverService.ListServers("")
	if err != nil {
		t.showKeyManagerError("Delete key", err)
		return
	}
	for _, server := range servers {
		if server.ManagedKeyID == key.ID {
			t.showKeyManagerError("Delete key", fmt.Errorf("key is still bound to server %q; unbind it first", server.Alias))
			return
		}
	}
	modal := tview.NewModal().
		SetText(fmt.Sprintf("Delete managed key %q?\n\nThis removes it from the encrypted vault.", key.Name)).
		AddButtons([]string{"Cancel", "Delete"}).
		SetDoneFunc(func(buttonIndex int, buttonLabel string) {
			if buttonIndex == 1 {
				if err := t.keyService.DeleteKey(key.ID); err != nil {
					t.showKeyManagerError("Delete key", err)
					return
				}
			}
			t.showKeyManager()
		})
	t.app.SetRoot(modal, true)
}

func (t *tui) showKeyManagerError(action string, err error) {
	modal := tview.NewModal().
		SetText(fmt.Sprintf("%s failed:\n\n%v", action, err)).
		AddButtons([]string{"Close"}).
		SetDoneFunc(func(buttonIndex int, buttonLabel string) { t.showKeyManager() })
	t.app.SetRoot(modal, true)
}
