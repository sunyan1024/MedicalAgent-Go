package user

import (
	"strings"
	"testing"

	"GoAI/config"
	"GoAI/internal/testutil"
	"GoAI/model"
	"GoAI/utils"
)

func TestReadAdminCredentials(t *testing.T) {
	for _, tc := range []struct {
		name, uid, email, password string
		wantErr                    bool
	}{
		{"unset", "", "", "", false},
		{"partial", "admin", "", "", true},
		{"valid", "admin", "admin@example.com", "test-password", false},
		{"invalid email", "admin", "invalid", "password", true},
		{"blank password", "admin", "admin@example.com", " ", true},
		{"long password", "admin", "admin@example.com", strings.Repeat("a", 73), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := readAdminCredentials(config.AdminConfig{UID: tc.uid, Email: tc.email, Password: tc.password})
			if (err != nil) != tc.wantErr {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestBootstrapAdminIsIdempotentAndRejectsConflicts(t *testing.T) {
	db := testutil.MySQL(t)
	if err := db.AutoMigrate(&model.User{}); err != nil {
		t.Fatal(err)
	}
	c := adminCredentials{"admin", "admin@example.com", "original-password"}
	if err := bootstrapAdmin(db, c); err != nil {
		t.Fatal(err)
	}
	c.password = "replacement-password"
	if err := bootstrapAdmin(db, c); err != nil {
		t.Fatal(err)
	}
	var account model.User
	if err := db.Where("uid = ?", c.uid).First(&account).Error; err != nil {
		t.Fatal(err)
	}
	if account.Role != model.RoleAdmin || utils.CompareHashAndPassword(account.Password, "original-password") != nil {
		t.Fatal("role or password changed")
	}
	if err := db.Model(&account).Update("role", model.RoleUser).Error; err != nil {
		t.Fatal(err)
	}
	if err := bootstrapAdmin(db, c); err == nil {
		t.Fatal("ordinary user must not be elevated")
	}
	if err := bootstrapAdmin(db, adminCredentials{"another", c.email, "password"}); err == nil {
		t.Fatal("email conflict accepted")
	}
	if err := bootstrapAdmin(db, adminCredentials{c.email, "other@example.com", "password"}); err == nil {
		t.Fatal("cross-identifier conflict accepted")
	}
}
