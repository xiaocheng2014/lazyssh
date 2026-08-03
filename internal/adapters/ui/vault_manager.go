package ui

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

func (t *tui) showVaultManager() {
	if t.vaultService == nil {
		t.showStatusTempColor("Vault management is unavailable", "#FF6B6B")
		return
	}
	text := fmt.Sprintf("Encrypted bundle:\n%s\n\nLocal password (not stored in Git):\n%s\n\nThis device uses the local password automatically. Configuration, metadata, and managed keys are decrypted only while LazySSH is running.", t.vaultService.VaultPath(), t.vaultService.LocalPasswordPath())
	modal := tview.NewModal().
		SetText(text).
		AddButtons([]string{"Close", "Change Password"}).
		SetDoneFunc(func(buttonIndex int, buttonLabel string) {
			if buttonIndex == 1 {
				t.showChangeVaultPasswordForm()
				return
			}
			t.returnToMain()
		})
	t.app.SetRoot(modal, true)
}

func (t *tui) showChangeVaultPasswordForm() {
	form := tview.NewForm()
	form.SetBorder(true).SetTitle(" Change Vault Password ").SetTitleAlign(tview.AlignCenter)
	newPassword := tview.NewInputField().SetLabel("New password:").SetMaskCharacter('*').SetFieldWidth(50)
	confirmation := tview.NewInputField().SetLabel("Confirm:").SetMaskCharacter('*').SetFieldWidth(50)
	form.AddFormItem(newPassword)
	form.AddFormItem(confirmation)
	form.AddTextView("", "Use at least 10 characters. Losing this password makes the encrypted bundle unrecoverable.", 0, 2, true, false)
	form.AddButton("Change", func() {
		password := []byte(newPassword.GetText())
		confirm := []byte(confirmation.GetText())
		defer clearPasswordBytes(password)
		defer clearPasswordBytes(confirm)
		newPassword.SetText("")
		confirmation.SetText("")
		if len(password) < 10 || strings.TrimSpace(string(password)) == "" {
			t.showVaultError(fmt.Errorf("password must contain at least 10 characters"))
			return
		}
		if !bytes.Equal(password, confirm) {
			t.showVaultError(fmt.Errorf("passwords do not match"))
			return
		}
		if err := t.vaultService.ChangePassword(password); err != nil {
			t.showVaultError(err)
			return
		}
		t.returnToMain()
		t.showStatusTemp("Vault password and local password file changed")
	})
	form.AddButton("Cancel", t.showVaultManager)
	form.SetCancelFunc(t.showVaultManager)
	form.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape {
			t.showVaultManager()
			return nil
		}
		return event
	})
	t.app.SetRoot(form, true)
	t.app.SetFocus(form)
}

func (t *tui) showVaultError(err error) {
	modal := tview.NewModal().
		SetText(fmt.Sprintf("Vault operation failed:\n\n%v", err)).
		AddButtons([]string{"Close"}).
		SetDoneFunc(func(buttonIndex int, buttonLabel string) { t.showVaultManager() })
	t.app.SetRoot(modal, true)
}

func clearPasswordBytes(value []byte) {
	for index := range value {
		value[index] = 0
	}
}
