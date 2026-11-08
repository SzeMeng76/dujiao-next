package integrationtest

import (
	"context"
	"errors"
	"testing"

	"github.com/dujiao-next/internal/constants"
	userdomain "github.com/dujiao-next/internal/modules/identity/user/domain"
	userauthapp "github.com/dujiao-next/internal/modules/identity/userauth/application"
	"github.com/dujiao-next/internal/telegramidentity"

	"gorm.io/gorm"
)

func createUpgradeTestUser(t *testing.T, db *gorm.DB, email string) *userdomain.User {
	t.Helper()
	user := &userdomain.User{Email: email, PasswordHash: "x", Status: constants.UserStatusActive}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("create user %q failed: %v", email, err)
	}
	return user
}

// upgrade_placeholder 只能由登录态的占位账号触发，公开发信入口必须拒绝且不发信。
func TestSendVerifyCodeRejectsUpgradePlaceholderPurpose(t *testing.T) {
	sender := &countingVerificationEmailSender{}
	svc, _, _ := newRegistrationDomainPolicyAuthServiceWithSender(t, sender)

	err := svc.SendVerifyCode(context.Background(), "stranger@example.com", constants.VerifyPurposeUpgradePlaceholder, constants.LocaleZhCN, "")
	if !errors.Is(err, userauthapp.ErrInvalidVerifyPurpose) {
		t.Fatalf("expected ErrInvalidVerifyPurpose, got %v", err)
	}
	if sender.sent != 0 {
		t.Fatalf("email must not be sent, sent=%d", sender.sent)
	}
}

func TestSendUpgradePlaceholderCodeForPlaceholderUser(t *testing.T) {
	sender := &countingVerificationEmailSender{}
	svc, _, db := newRegistrationDomainPolicyAuthServiceWithSender(t, sender)
	user := createUpgradeTestUser(t, db, telegramidentity.BuildPlaceholderEmail("1001"))

	if err := svc.SendUpgradePlaceholderCode(context.Background(), user.ID, " New@Example.com ", constants.LocaleZhCN); err != nil {
		t.Fatalf("send upgrade code failed: %v", err)
	}
	if sender.sent != 1 {
		t.Fatalf("expected 1 email sent, got %d", sender.sent)
	}
}

func TestSendUpgradePlaceholderCodeRejectsNonPlaceholderUser(t *testing.T) {
	sender := &countingVerificationEmailSender{}
	svc, _, db := newRegistrationDomainPolicyAuthServiceWithSender(t, sender)
	user := createUpgradeTestUser(t, db, "real@example.com")

	err := svc.SendUpgradePlaceholderCode(context.Background(), user.ID, "other@example.com", constants.LocaleZhCN)
	if !errors.Is(err, userauthapp.ErrUserOAuthAlreadyBound) {
		t.Fatalf("expected ErrUserOAuthAlreadyBound, got %v", err)
	}
	if sender.sent != 0 {
		t.Fatalf("email must not be sent, sent=%d", sender.sent)
	}
}

func TestSendUpgradePlaceholderCodeRejectsTakenEmail(t *testing.T) {
	sender := &countingVerificationEmailSender{}
	svc, _, db := newRegistrationDomainPolicyAuthServiceWithSender(t, sender)
	user := createUpgradeTestUser(t, db, telegramidentity.BuildPlaceholderEmail("1002"))
	createUpgradeTestUser(t, db, "taken@example.com")

	err := svc.SendUpgradePlaceholderCode(context.Background(), user.ID, "taken@example.com", constants.LocaleZhCN)
	if !errors.Is(err, userauthapp.ErrEmailExists) {
		t.Fatalf("expected ErrEmailExists, got %v", err)
	}
	if sender.sent != 0 {
		t.Fatalf("email must not be sent, sent=%d", sender.sent)
	}
}

func TestSendUpgradePlaceholderCodeRejectsInvalidOrPlaceholderTarget(t *testing.T) {
	sender := &countingVerificationEmailSender{}
	svc, _, db := newRegistrationDomainPolicyAuthServiceWithSender(t, sender)
	user := createUpgradeTestUser(t, db, telegramidentity.BuildPlaceholderEmail("1003"))

	for _, target := range []string{"not-an-email", telegramidentity.BuildPlaceholderEmail("9999")} {
		err := svc.SendUpgradePlaceholderCode(context.Background(), user.ID, target, constants.LocaleZhCN)
		if !errors.Is(err, userauthapp.ErrInvalidEmail) {
			t.Fatalf("target %q: expected ErrInvalidEmail, got %v", target, err)
		}
	}
	if sender.sent != 0 {
		t.Fatalf("email must not be sent, sent=%d", sender.sent)
	}
}

func TestSendUpgradePlaceholderCodeRejectsUnknownUser(t *testing.T) {
	sender := &countingVerificationEmailSender{}
	svc, _, _ := newRegistrationDomainPolicyAuthServiceWithSender(t, sender)

	for _, userID := range []uint{0, 424242} {
		err := svc.SendUpgradePlaceholderCode(context.Background(), userID, "new@example.com", constants.LocaleZhCN)
		if !errors.Is(err, userauthapp.ErrNotFound) {
			t.Fatalf("userID %d: expected ErrNotFound, got %v", userID, err)
		}
	}
	if sender.sent != 0 {
		t.Fatalf("email must not be sent, sent=%d", sender.sent)
	}
}
