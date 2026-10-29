package application

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/auth/domain"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

const (
	// MaxLoginFail is the number of consecutive login failures allowed before locking.
	MaxLoginFail = 5
	// LoginLockSecs is the failure-count window (lock duration).
	LoginLockSecs = 900
)

// Business errors (migrated from the legacy service; semantics stay consistent with the handler error mapping).
var (
	ErrUserExists         = errors.New("user already exists")
	ErrEmailExists        = errors.New("email already exists")
	ErrInvalidCredentials = errors.New("invalid username or password")
	ErrUserLocked         = errors.New("account locked due to too many failed attempts")
)

// dummyHash is used to perform an equivalent-cost bcrypt comparison when the user does not exist, eliminating the timing side channel
// so attackers cannot probe whether a username exists via response-time differences.
var dummyHash = func() []byte {
	h, _ := bcrypt.GenerateFromPassword([]byte("dummy-timing-equalizer"), bcrypt.DefaultCost)
	return h
}()

// loginFailKey builds the login-failure count key. Lowercase-private (consistent with legacy; used only in this package).
func loginFailKey(username string) string {
	return fmt.Sprintf("login:fail:%s", username)
}

// JwtBlacklistKey returns the blacklist key for a jti. Reused by the middleware.
func JwtBlacklistKey(jti string) string {
	return fmt.Sprintf("jwt:blacklist:%s", jti)
}

// AuthApp carries the business logic for user registration, login, and logout (application service).
type AuthApp struct {
	repo domain.UserRepository
	rdb  domain.RedisPort
	auth *domain.AuthService
}

// NewAuthApp creates an AuthApp. rdb may be nil (no-Redis degradation: skip cache/blacklist, business proceeds).
func NewAuthApp(repo domain.UserRepository, rdb domain.RedisPort, auth *domain.AuthService) *AuthApp {
	return &AuthApp{repo: repo, rdb: rdb, auth: auth}
}

// Register creates a new user: duplicate checks (username + email), bcrypt hashing, persistence.
func (a *AuthApp) Register(ctx context.Context, username, password, email string) error {
	if _, err := a.repo.FindByUsername(ctx, username); err == nil {
		return ErrUserExists
	} else if !errors.Is(err, domain.ErrUserNotFound) {
		return err
	}
	if _, err := a.repo.FindByEmail(ctx, email); err == nil {
		return ErrEmailExists
	} else if !errors.Is(err, domain.ErrUserNotFound) {
		return err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	err = a.repo.Create(ctx, &domain.User{
		Username:     username,
		PasswordHash: string(hash),
		Email:        email,
	})
	// Under concurrency, the race between the duplicate check and the insert is covered by the unique index.
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return ErrUserExists
	}
	return err
}

// Login validates the username/password and issues a token, with failure-count rate limiting.
func (a *AuthApp) Login(ctx context.Context, username, password string) (string, error) {
	if a.rdb != nil {
		v, _ := a.rdb.Get(ctx, loginFailKey(username))
		failCount, _ := strconv.Atoi(v) // missing key / non-numeric counts as 0, consistent with legacy Get().Int() semantics
		if failCount >= MaxLoginFail {
			return "", ErrUserLocked
		}
	}

	user, err := a.repo.FindByUsername(ctx, username)
	if err != nil {
		// Perform an equivalent bcrypt comparison even when the user does not exist, so user existence cannot be probed via response timing.
		_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
		a.recordLoginFail(ctx, username)
		return "", ErrInvalidCredentials
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		a.recordLoginFail(ctx, username)
		return "", ErrInvalidCredentials
	}

	if a.rdb != nil {
		a.rdb.Del(ctx, loginFailKey(username))
	}
	return a.auth.GenerateToken(user.Username)
}

func (a *AuthApp) recordLoginFail(ctx context.Context, username string) {
	if a.rdb == nil {
		return
	}
	key := loginFailKey(username)
	n, err := a.rdb.Incr(ctx, key)
	if err != nil {
		return
	}
	if n == 1 {
		a.rdb.Expire(ctx, key, time.Duration(LoginLockSecs)*time.Second)
	}
}

// LogoutByJTI writes the given jti into the Redis blacklist with a TTL equal to the token's remaining validity.
// jti and exp are parsed by the JWT middleware and injected into the context, avoiding re-parsing the token at logout.
func (a *AuthApp) LogoutByJTI(ctx context.Context, jti string, exp time.Time) error {
	if a.rdb == nil || jti == "" {
		return nil
	}
	ttl := time.Until(exp)
	if ttl <= 0 {
		return nil
	}
	return a.rdb.Set(ctx, JwtBlacklistKey(jti), "1", ttl)
}
