package service

import (
	"context"
	"errors"
	"finote/internal/payment/model"
	"testing"
)

func TestNormalizeUserID(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"123", "123"}, {"00123", "123"}, {"+123", "123"}, {" \t+00123\n", "123"},
		{"9223372036854775807", "9223372036854775807"},
	} {
		got, err := NormalizeUserID(tc.input)
		if err != nil || got != tc.want {
			t.Errorf("%q => %q, %v", tc.input, got, err)
		}
	}
	for _, input := range []string{"", " ", "0", "+0", "-1", "123.0", "1e3", "1_23", "12 3", "１２３", "abc", "9223372036854775808"} {
		if _, err := NormalizeUserID(input); !errors.Is(err, ErrValidation) {
			t.Errorf("%q: %v", input, err)
		}
	}
}
func TestInvalidOwnerRejectedBeforeDatabase(t *testing.T) {
	s := New(nil)
	for _, id := range []string{"bad", " ", "0", "-1"} {
		if _, err := s.CreatePayment(context.Background(), model.Payment{UserID: id, Amount: 400, Category: "еда"}); !errors.Is(err, ErrValidation) {
			t.Fatal(err)
		}
		f := model.Filter{UserID: id}
		if _, err := s.ListPayments(context.Background(), f); !errors.Is(err, ErrValidation) {
			t.Fatal(err)
		}
		if _, err := s.FindPayments(context.Background(), f); !errors.Is(err, ErrValidation) {
			t.Fatal(err)
		}
		if _, _, err := s.SumPayments(context.Background(), f); !errors.Is(err, ErrValidation) {
			t.Fatal(err)
		}
	}
	f, err := normalizeFilter(model.Filter{})
	if err != nil || f.UserID != "" {
		t.Fatal("empty admin filter must remain valid")
	}
}
