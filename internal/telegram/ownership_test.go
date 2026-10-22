package telegram

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"finote/internal/payment/model"
	"finote/internal/payment/repo"
	"finote/internal/payment/service"
)

func TestOwnedRepositoryNeverReadsOrMutatesForeignExpenses(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	addExpense(t, s, 1, "10")
	addExpense(t, s, 2, "20")
	var foreign string
	if err := s.db.QueryRow("SELECT id FROM payments WHERE user_id='20'").Scan(&foreign); err != nil {
		t.Fatal(err)
	}
	tx, err := s.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	r, err := repo.NewOwnedTx(tx, "10")
	if err != nil {
		t.Fatal(err)
	}
	svc := service.New(r)
	if _, err = svc.GetPayment(ctx, foreign); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("foreign get", err)
	}
	amount := 700.0
	if _, err = svc.PatchPayment(ctx, foreign, model.UpdatePayment{Amount: &amount}); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("foreign patch", err)
	}
	if err = svc.DeletePayment(ctx, foreign); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("foreign delete", err)
	}
	items, err := svc.FindPayments(ctx, model.Filter{UserID: "20"})
	if err != nil || len(items) != 1 || items[0].UserID != "10" {
		t.Fatal(items, err)
	}
	total, count, err := svc.SumPayments(ctx, model.Filter{UserID: "20"})
	if err != nil || total != "400.00" || count != 1 {
		t.Fatal(total, count, err)
	}
	p, err := svc.CreatePayment(ctx, model.Payment{UserID: "20", Category: "еда", Amount: 300})
	if err != nil || p.UserID != "10" {
		t.Fatal(p, err)
	}
}

func TestAmbiguousDeleteCancelAndExpiry(t *testing.T) {
	for _, mode := range []string{"cancel", "expiry", "selection"} {
		t.Run(mode, func(t *testing.T) {
			s := testStore(t)
			ctx := context.Background()
			addExpense(t, s, 1, "10")
			addExpense(t, s, 2, "10")
			if _, err := s.Process(ctx, "123", 3, "10", Intent{Action: "delete", FilterCategory: "еда"}); err != nil {
				t.Fatal(err)
			}
			assertCount(t, s, "payments", 2)
			if _, err := s.Process(ctx, "123", 4, "10", Intent{Action: "clarify", answer: "99"}); err != nil {
				t.Fatal(err)
			}
			assertCount(t, s, "payments", 2)
			if mode == "cancel" {
				if _, err := s.Process(ctx, "123", 5, "10", Intent{Action: "clarify", cancelDialog: true}); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "expiry" {
				if _, err := s.db.Exec("UPDATE telegram_dialogs SET expires_at=now()-interval '1 second'"); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := s.Process(ctx, "123", 6, "10", Intent{Action: "clarify", answer: "2"}); err != nil {
				t.Fatal(err)
			}
			want := 2
			if mode == "selection" {
				want = 1
			}
			assertCount(t, s, "payments", want)
		})
	}
}
