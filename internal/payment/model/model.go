package model

import (
	"errors"
	"time"
)

var ErrConflict = errors.New("payment version conflict")

type User struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	Username    string `json:"username,omitempty"`
}

type Payment struct {
	Version   int64     `json:"version"`
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	Category  string    `json:"category"`
	Amount    float64   `json:"amount"`
	Comment   string    `json:"comment,omitempty"`
	SpentAt   time.Time `json:"spent_at"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type UpdatePayment struct {
	ExpectedVersion *int64     `json:"expected_version,omitempty"`
	Category        *string    `json:"category"`
	Amount          *float64   `json:"amount"`
	Comment         *string    `json:"comment"`
	SpentAt         *time.Time `json:"spent_at"`
}
