package model

import "time"

type Payment struct {
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
	Category *string    `json:"category"`
	Amount   *float64   `json:"amount"`
	Comment  *string    `json:"comment"`
	SpentAt  *time.Time `json:"spent_at"`
}
