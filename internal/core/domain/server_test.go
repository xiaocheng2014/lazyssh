package domain

import "testing"

func TestSSHConnectionAlias(t *testing.T) {
	if got := SSHConnectionAlias("production-db"); got != "production-db" {
		t.Fatalf("ASCII alias changed to %q", got)
	}
	first := SSHConnectionAlias("生产数据库")
	second := SSHConnectionAlias("生产数据库")
	if first != second || !IsInternalSSHConnectionAlias(first) {
		t.Fatalf("Chinese alias did not get a stable internal alias: %q %q", first, second)
	}
	if first == SSHConnectionAlias("香港服务器") {
		t.Fatal("different display aliases received the same internal alias")
	}
}
