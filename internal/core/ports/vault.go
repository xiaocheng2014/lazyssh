package ports

type VaultService interface {
	ChangePassword(newPassword []byte) error
	VaultPath() string
	LocalPasswordPath() string
}
