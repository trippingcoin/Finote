package model

import "time"

type Filter struct {
	UserID   string
	Category string
	From     *time.Time
	To       *time.Time
	Limit    int
	Offset   int
}

type PaymentList struct {
	Items   []Payment `json:"items"`
	Total   int64     `json:"total"`
	Amount  string    `json:"amount"`
	Members int64     `json:"members"`
	Limit   int       `json:"limit"`
	Offset  int       `json:"offset"`
}
