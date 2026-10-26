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
	// MaxLoginFail 允许的连续登录失败次数，超过即锁定。
	MaxLoginFail = 5
	// LoginLockSecs 失败计数窗口（锁定时长）。
	LoginLockSecs = 900
)

func loginFailKey(username string) string {
	return fmt.Sprintf("login:fail:%s", username)
}

// JwtBlacklistKey 返回 jti 对应的黑名单键。供中间件复用。
func JwtBlacklistKey(jti string) string {
	return fmt.Sprintf("jwt:blacklist:%s", jti)
}

// 业务错误。
var (
	ErrUserExists         = errors.New("user already exists")
	ErrEmailExists        = errors.New("email already exists")
	ErrInvalidCredentials = errors.New("invalid username or password")
	ErrUserLocked         = errors.New("account locked due to too many failed attempts")
)

// dummyHash 用于用户不存在时执行一次等价开销的 bcrypt 比较，消除时序侧信道，
// 避免攻击者通过响应时间差异探测用户名是否存在。
var dummyHash = func() []byte {
	h, _ := bcrypt.GenerateFromPassword([]byte("dummy-timing-equalizer"), bcrypt.DefaultCost)
	return h
}()

// UserService 承载用户注册、登录、登出业务逻辑。
type UserService struct {
	repo repository.UserRepository
	rdb  *redis.Client
	auth *AuthService
}

// NewUserService 创建 UserService。
func NewUserService(repo repository.UserRepository, rdb *redis.Client, auth *AuthService) *UserService {
	return &UserService{repo: repo, rdb: rdb, auth: auth}
}

// Register 注册新用户：查重（用户名 + 邮箱）、bcrypt 哈希、入库。
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
	// 并发下查重与插入之间的竞态由唯一索引兜底。
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return ErrUserExists
	}
	return err
}

// Login 校验用户名密码并签发 token，含失败计数限流。
func (s *UserService) Login(ctx context.Context, username, password string) (string, error) {
	if s.rdb != nil {
		if failCount, _ := s.rdb.Get(ctx, loginFailKey(username)).Int(); failCount >= MaxLoginFail {
			return "", ErrUserLocked
		}
	}

	user, err := s.repo.FindByUsername(ctx, username)
	if err != nil {
		// 用户不存在时也做一次等价 bcrypt 比较，避免通过响应时序探测用户是否存在。
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

// LogoutByJTI 将指定 jti 写入 Redis 黑名单，TTL 为 token 剩余有效期。
// jti 与 exp 由 JWT 中间件解析后注入 gin.Context，避免在登出处重复解析 token。
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
