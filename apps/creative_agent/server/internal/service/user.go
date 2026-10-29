package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/model"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/repository"

	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

const (
	// MaxLoginFail is the number of consecutive login failures allowed before locking.
	MaxLoginFail = 5
	// LoginLockSecs is the failure-count window (lock duration).
	LoginLockSecs = 900
)

func loginFailKey(username string) string {
	return fmt.Sprintf("login:fail:%s", username)
}

// JwtBlacklistKey returns the blacklist key for a jti. Reused by the middleware.
func JwtBlacklistKey(jti string) string {
	return fmt.Sprintf("jwt:blacklist:%s", jti)
}

// Business errors.
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

// UserService carries the business logic for user registration, login, and logout.
type UserService struct {
	repo repository.UserRepository
	rdb  *redis.Client
	auth *AuthService
}

// NewUserService creates a UserService.
func NewUserService(repo repository.UserRepository, rdb *redis.Client, auth *AuthService) *UserService {
	return &UserService{repo: repo, rdb: rdb, auth: auth}
}

// Register creates a new user: duplicate checks (username + email), bcrypt hashing, persistence.
func (s *UserService) Register(ctx context.Context, username, password, email string) error {
	if _, err := s.repo.FindByUsername(ctx, username); err == nil {
		return ErrUserExists
	} else if !errors.Is(err, repository.ErrUserNotFound) {
		return err
	}
	if _, err := s.repo.FindByEmail(ctx, email); err == nil {
		return ErrEmailExists
	} else if !errors.Is(err, repository.ErrUserNotFound) {
		return err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	err = s.repo.Create(ctx, &model.User{
		Username: username,
		Password: string(hash),
		Email:    email,
	})
	// Under concurrency, the race between the duplicate check and the insert is covered by the unique index.
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return ErrUserExists
	}
	return err
}

// Login validates the username/password and issues a token, with failure-count rate limiting.
func (s *UserService) Login(ctx context.Context, username, password string) (string, error) {
	if s.rdb != nil {
		if failCount, _ := s.rdb.Get(ctx, loginFailKey(username)).Int(); failCount >= MaxLoginFail {
			return "", ErrUserLocked
		}
	}

	user, err := s.repo.FindByUsername(ctx, username)
	if err != nil {
		// Perform an equivalent bcrypt comparison even when the user does not exist, so user existence cannot be probed via response timing.
		_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
		s.recordLoginFail(ctx, username)
		return "", ErrInvalidCredentials
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(password)); err != nil {
		s.recordLoginFail(ctx, username)
		return "", ErrInvalidCredentials
	}

	if s.rdb != nil {
		s.rdb.Del(ctx, loginFailKey(username))
	}
	return s.auth.GenerateToken(user.Username, int64(user.ID))
}

func (s *UserService) recordLoginFail(ctx context.Context, username string) {
	if s.rdb == nil {
		return
	}
	key := loginFailKey(username)
	n, err := s.rdb.Incr(ctx, key).Result()
	if err != nil {
		return
	}
	if n == 1 {
		s.rdb.Expire(ctx, key, time.Duration(LoginLockSecs)*time.Second)
	}
}

// LogoutByJTI writes the given jti into the Redis blacklist with a TTL equal to the token's remaining validity.
// jti and exp are parsed by the JWT middleware and injected into the context, avoiding re-parsing the token at logout.
func (s *UserService) LogoutByJTI(ctx context.Context, jti string, exp time.Time) error {
	if s.rdb == nil || jti == "" {
		return nil
	}
	ttl := time.Until(exp)
	if ttl <= 0 {
		return nil
	}
	return s.rdb.Set(ctx, JwtBlacklistKey(jti), 1, ttl).Err()
}
