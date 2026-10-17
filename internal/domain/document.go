package domain

import "time"

type DocID uint64

type Document struct {
	ID      DocID  `json:"id"`
	URL     string `json:"source"`
	Title   string `json:"title"`
	Content string `json:"content"`

	Language string `json:"language"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
