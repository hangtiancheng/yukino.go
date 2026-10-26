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
	// MaxLoginFail 允许的连续登录失败次数，超过即锁定。
	MaxLoginFail = 5
	// LoginLockSecs 失败计数窗口（锁定时长）。
	LoginLockSecs = 900
)

// 业务错误（迁移自 legacy service，语义与 handler 错误映射保持一致）。
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

// loginFailKey 构造登录失败计数 key。小写私有（与 legacy 一致，仅本包使用）。
func loginFailKey(username string) string {
	return fmt.Sprintf("login:fail:%s", username)
}

// JwtBlacklistKey 返回 jti 对应的黑名单键。供 middleware 复用。
func JwtBlacklistKey(jti string) string {
	return fmt.Sprintf("jwt:blacklist:%s", jti)
}

// AuthApp 承载用户注册、登录、登出业务逻辑（应用服务）。
type AuthApp struct {
	repo domain.UserRepository
	rdb  domain.RedisPort
	auth *domain.AuthService
}

// NewAuthApp 创建 AuthApp。rdb 可为 nil（无 Redis 时降级：跳过缓存/黑名单，业务照常）。
func NewAuthApp(repo domain.UserRepository, rdb domain.RedisPort, auth *domain.AuthService) *AuthApp {
	return &AuthApp{repo: repo, rdb: rdb, auth: auth}
}

// Register 注册新用户：查重（用户名 + 邮箱）、bcrypt 哈希、入库。
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
	// 并发下查重与插入之间的竞态由唯一索引兜底。
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return ErrUserExists
	}
	return err
}

// Login 校验用户名密码并签发 token，含失败计数限流。
func (a *AuthApp) Login(ctx context.Context, username, password string) (string, error) {
	if a.rdb != nil {
		v, _ := a.rdb.Get(ctx, loginFailKey(username))
		failCount, _ := strconv.Atoi(v) // 键不存在/非数字时视为 0，与 legacy Get().Int() 语义一致
		if failCount >= MaxLoginFail {
			return "", ErrUserLocked
		}
	}

	user, err := a.repo.FindByUsername(ctx, username)
	if err != nil {
		// 用户不存在时也做一次等价 bcrypt 比较，避免通过响应时序探测用户是否存在。
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

// LogoutByJTI 将指定 jti 写入 Redis 黑名单，TTL 为 token 剩余有效期。
// jti 与 exp 由 JWT 中间件解析后注入 gin.Context，避免在登出处重复解析 token。
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
