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
		t.showStatusTempColor("密码管理功能当前不可用", "#FF6B6B")
		return
	}
	text := fmt.Sprintf("加密仓库：\n%s\n\n本地密码文件（不会存入 Git）：\n%s\n\n本设备会自动使用本地密码。配置、元数据和托管密钥只会在 LazySSH 运行期间解密。", t.vaultService.VaultPath(), t.vaultService.LocalPasswordPath())
	modal := tview.NewModal().
		SetText(text).
		AddButtons([]string{"关闭", "测试密码", "修改密码"}).
		SetDoneFunc(func(buttonIndex int, buttonLabel string) {
			switch buttonLabel {
			case "测试密码":
				t.showTestVaultPasswordForm()
				return
			case "修改密码":
				t.showChangeVaultPasswordForm()
				return
			}
			t.returnToMain()
		})
	t.app.SetRoot(modal, true)
}

func (t *tui) showChangeVaultPasswordForm() {
	form := tview.NewForm()
	form.SetBorder(true).SetTitle(" 修改加密密码 ").SetTitleAlign(tview.AlignCenter)
	newPassword := tview.NewInputField().SetLabel("新密码：").SetMaskCharacter('*').SetFieldWidth(50)
	confirmation := tview.NewInputField().SetLabel("确认密码：").SetMaskCharacter('*').SetFieldWidth(50)
	form.AddFormItem(newPassword)
	form.AddFormItem(confirmation)
	form.AddTextView("", "密码至少需要 10 个字符。密码丢失后将无法恢复加密仓库。", 0, 2, true, false)
	form.AddButton("修改", func() {
		password := []byte(newPassword.GetText())
		confirm := []byte(confirmation.GetText())
		defer clearPasswordBytes(password)
		defer clearPasswordBytes(confirm)
		newPassword.SetText("")
		confirmation.SetText("")
		if len(password) < 10 || strings.TrimSpace(string(password)) == "" {
			t.showVaultError(fmt.Errorf("密码至少需要 10 个字符"))
			return
		}
		if !bytes.Equal(password, confirm) {
			t.showVaultError(fmt.Errorf("两次输入的密码不一致"))
			return
		}
		if err := t.vaultService.ChangePassword(password); err != nil {
			t.showVaultError(err)
			return
		}
		t.returnToMain()
		t.showStatusTemp("加密密码和本地密码文件已更新")
	})
	form.AddButton("取消", t.showVaultManager)
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

func (t *tui) showTestVaultPasswordForm() {
	form := tview.NewForm()
	form.SetBorder(true).SetTitle(" 测试加密密码 ").SetTitleAlign(tview.AlignCenter)
	passwordField := tview.NewInputField().SetLabel("密码：").SetMaskCharacter('*').SetFieldWidth(50)
	form.AddFormItem(passwordField)
	form.AddTextView("", "仅尝试解密仓库以验证密码，不会修改任何数据。", 0, 2, true, false)
	form.AddButton("测试", func() {
		password := []byte(passwordField.GetText())
		defer clearPasswordBytes(password)
		passwordField.SetText("")
		if err := t.vaultService.VerifyPassword(password); err != nil {
			t.showVaultError(err)
			return
		}
		t.returnToMain()
		t.showStatusTemp("密码正确，可以正常解密配置仓库")
	})
	form.AddButton("取消", t.showVaultManager)
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
		SetText(fmt.Sprintf("密码操作失败：\n\n%v", err)).
		AddButtons([]string{"关闭"}).
		SetDoneFunc(func(buttonIndex int, buttonLabel string) { t.showVaultManager() })
	t.app.SetRoot(modal, true)
}

func clearPasswordBytes(value []byte) {
	for index := range value {
		value[index] = 0
	}
}
