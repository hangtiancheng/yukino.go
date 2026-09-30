package model

import "time"

type User struct {
	ID        int64
	Name      string
	Email     string
	Username  string
	Password  string
	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt *time.Time
}

type Session struct {
	ID        string
	Username  string
	Title     string
	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt *time.Time
}

type Message struct {
	ID        int64
	SessionID string
	Username  string
	Content   string
	IsUser    bool
	CreatedAt time.Time
}

type SessionDTO struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

type History struct {
	IsUser  bool   `json:"is_user"`
	Content string `json:"content"`
}
