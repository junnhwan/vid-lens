package service

import (
	"errors"
	"fmt"
	"time"

	"golang.org/x/crypto/bcrypt"
	"vid-lens/internal/config"
	"vid-lens/internal/model"
	"vid-lens/internal/pkg/jwt"
	"vid-lens/internal/repository"
)

var (
	ErrUserExists         = errors.New("该用户名已被注册")
	ErrInvalidCredentials = errors.New("用户名或密码错误")
)

type UserService struct {
	repo   *repository.UserRepository
	jwtCfg config.JWTConfig
}

func NewUserService(repo *repository.UserRepository, jwtCfg config.JWTConfig) *UserService {
	return &UserService{repo: repo, jwtCfg: jwtCfg}
}

// Register 用户注册
// 面试亮点：对比原项目明文存储密码，这里使用 bcrypt 加密
func (s *UserService) Register(username, password, nickname string) (*model.User, string, error) {
	existing, _ := s.repo.FindByUsername(username)
	if existing != nil {
		return nil, "", ErrUserExists
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), 10)
	if err != nil {
		return nil, "", err
	}

	if nickname == "" {
		nickname = "用户" + fmt.Sprintf("%d", time.Now().UnixMilli())
	}

	user := &model.User{
		Username:     username,
		PasswordHash: string(hashedPassword),
		Nickname:     nickname,
		Role:         "USER",
	}

	if err := s.repo.Create(user); err != nil {
		return nil, "", err
	}

	token, err := jwt.GenerateToken(user.ID, user.Username, user.Role,
		s.jwtCfg.Secret, s.jwtCfg.ExpireHours)
	if err != nil {
		return nil, "", err
	}

	return user, token, nil
}

// Login 用户登录
func (s *UserService) Login(username, password string) (*model.User, string, error) {
	user, err := s.repo.FindByUsername(username)
	if err != nil {
		return nil, "", ErrInvalidCredentials
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return nil, "", ErrInvalidCredentials
	}

	token, err := jwt.GenerateToken(user.ID, user.Username, user.Role,
		s.jwtCfg.Secret, s.jwtCfg.ExpireHours)
	if err != nil {
		return nil, "", err
	}

	return user, token, nil
}

// GetUserByID 根据 ID 获取用户
func (s *UserService) GetUserByID(id int64) (*model.User, error) {
	return s.repo.FindByID(id)
}
