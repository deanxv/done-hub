package model

import (
	"errors"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func setupUserFillTestDB(t *testing.T) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&User{}); err != nil {
		t.Fatalf("migrate users: %v", err)
	}
	orig := DB
	DB = db
	t.Cleanup(func() { DB = orig })
}

// 查不到用户时，调用方（如 OIDC 登录）需要用 errors.Is(err, gorm.ErrRecordNotFound)
// 判断是否继续走「按用户名匹配 → 自动注册」，提示文案保持不变。
func TestFillUserBy_NotFoundMatchesRecordNotFound(t *testing.T) {
	setupUserFillTestDB(t)

	cases := map[string]func() error{
		"Id":       func() error { u := User{Id: 42}; return u.FillUserById() },
		"Email":    func() error { u := User{Email: "nobody@example.com"}; return u.FillUserByEmail() },
		"OidcId":   func() error { u := User{OidcId: "sub-1"}; return u.FillUserByOidcId() },
		"LinuxDO":  func() error { u := User{LinuxDoId: 7}; return u.FillUserByLinuxDOId() },
		"Username": func() error { u := User{Username: "nobody"}; return u.FillUserByUsername() },
	}
	for name, fill := range cases {
		t.Run(name, func(t *testing.T) {
			err := fill()
			if err == nil {
				t.Fatal("expected an error for a missing user")
			}
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				t.Fatalf("errors.Is(err, gorm.ErrRecordNotFound) = false, err = %q", err)
			}
			if err.Error() != "没有找到用户！" {
				t.Fatalf("message changed: %q", err.Error())
			}
		})
	}
}

func TestFillUserByOidcId_Found(t *testing.T) {
	setupUserFillTestDB(t)
	if err := DB.Create(&User{Username: "alice", OidcId: "sub-1", AccessToken: "token-alice"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	u := User{OidcId: "sub-1"}
	if err := u.FillUserByOidcId(); err != nil {
		t.Fatalf("FillUserByOidcId: %v", err)
	}
	if u.Username != "alice" {
		t.Fatalf("username = %q, want alice", u.Username)
	}
}
