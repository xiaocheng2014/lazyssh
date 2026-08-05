package ports

type VaultService interface {
	ChangePassword(newPassword []byte) error
	VerifyPassword(password []byte) error
	VaultPath() string
	LocalPasswordPath() string
}
