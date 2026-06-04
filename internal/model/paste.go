package model

import "time"

type Paste struct {
	ID        string     `json:"id"`
	Content   string     `json:"content"`
	Language  string     `json:"language"`
	ExpiresAt *time.Time `json:"expires_at"`
	CreatedAt time.Time  `json:"created_at"`
	ViewCount int        `json:"view_count"`
}
