package model

import (
	"time"

	"gorm.io/gorm"
)

// 用户角色常量。DEMO 是给陌生人登录的只读演示账号：AI 配置不可改不可见细节、
// 资源只读（禁上传/触发异步处理），仅保留已处理视频的问答。
const (
	RoleUser  = "USER"
	RoleAdmin = "ADMIN"
	RoleDemo  = "DEMO"
)

// User 用户模型
type User struct {
	ID           int64          `gorm:"primaryKey;autoIncrement" json:"id"`
	Username     string         `gorm:"type:varchar(50);uniqueIndex;not null" json:"username"`
	PasswordHash string         `gorm:"column:password_hash;type:varchar(255);not null" json:"-"`
	Nickname     string         `gorm:"type:varchar(100)" json:"nickname"`
	Avatar       string         `gorm:"type:varchar(500)" json:"avatar"`
	Role         string         `gorm:"type:varchar(20);default:'USER'" json:"role"` // USER / ADMIN / DEMO
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
	DeletedAt    gorm.DeletedAt `gorm:"index" json:"-"`
}

// TableName 指定表名
func (User) TableName() string {
	return "users"
}
