package ui

import (
	"github.com/rivo/tview"
	"github.com/xiaocheng2014/lazyssh/internal/core/ports"
)

func (t *tui) showTransferForm(upload bool) {
	server, ok := t.serverList.GetSelectedServer()
	if !ok {
		t.showStatusTempColor("Select a server first", "#FF6B6B")
		return
	}
	form := tview.NewForm()
	title := " Upload to "
	if !upload {
		title = " Download from "
	}
	form.SetBorder(true).SetTitle(title + escapeForTview(server.Alias) + " ").SetTitleAlign(tview.AlignCenter)
	var localPath, remotePath string
	form.AddInputField("Local path", "", 80, nil, func(value string) { localPath = value })
	form.AddInputField("Remote absolute path", "", 80, nil, func(value string) { remotePath = value })
	var options ports.TransferOptions
	form.AddCheckbox("Recursive directory", false, func(value bool) { options.Recursive = value })
	form.AddCheckbox("Overwrite existing file", false, func(value bool) { options.Overwrite = value })
	form.AddCheckbox("Legacy SCP (no SFTP)", false, func(value bool) { options.Legacy = value })
	form.AddTextView("", "Remote path must be absolute. An existing directory is never overwritten. Esc cancels.", 0, 2, true, false)
	form.AddButton("Transfer", func() {
		var transferErr error
		t.app.Suspend(func() {
			if upload {
				transferErr = t.serverService.Upload(server.Alias, localPath, remotePath, options)
			} else {
				transferErr = t.serverService.Download(server.Alias, remotePath, localPath, options)
			}
		})
		t.returnToMain()
		if transferErr != nil {
			modal := tview.NewModal().SetText("Transfer failed:\n" + transferErr.Error()).AddButtons([]string{"OK"})
			modal.SetDoneFunc(func(_ int, _ string) { t.returnToMain() })
			t.app.SetRoot(modal, true)
			t.app.SetFocus(modal)
			return
		}
		t.showStatusTemp("Transfer complete")
	})
	form.AddButton("Cancel", func() { t.returnToMain() })
	form.SetCancelFunc(func() { t.returnToMain() })
	t.app.SetRoot(form, true)
	t.app.SetFocus(form)
}
