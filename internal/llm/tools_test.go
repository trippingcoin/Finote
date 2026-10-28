package llm

import "testing"

func TestToolContracts(t *testing.T) {
	for _, tc := range []struct{ name, args, action string }{
		{"create_payment", `{"amount":350,"category":"еда","comment":"кофе"}`, "create"},
		{"create_payment", `{"category":"еда","comment":"кофе"}`, "draft_create"},
		{"list_payments", `{"list_index":2}`, "list"},
		{"sum_payments", `{"filter_category":"еда","from":"2026-10-01","to":"2026-11-01"}`, "sum"},
		{"update_payment", `{"latest":true,"amount":450}`, "update"},
		{"delete_payment", `{"search":"такси"}`, "delete"},
		{"get_payment_id", `{"selected":true}`, "id"},
		{"ask_clarification", `{"question":"Какую трату изменить?"}`, "clarify"},
	} {
		t.Run(tc.name+tc.action, func(t *testing.T) {
			command, err := decodeTool(tc.name, tc.args)
			if err != nil || command.Action != tc.action {
				t.Fatalf("command=%+v err=%v", command, err)
			}
		})
	}
}

func TestToolArgumentsCannotEscapeTheirContract(t *testing.T) {
	for _, tc := range []struct{ name, args string }{
		{"sql", `{}`},
		{"create_payment", `{"amount":350,"category":"еда","user_id":"20"}`},
		{"create_payment", `{"amount":350,"category":"еда","action":"delete"}`},
		{"create_payment", `{"amount":350,"category":"еда","cancelDialog":true}`},
		{"create_payment", `{"amount":350,"category":"еда","id":"00000000-0000-4000-8000-000000000001"}`},
		{"create_payment", `{"category":"еда","comment":"кофе","amount":null}`},
		{"create_payment", `{"amount":350}`},
		{"create_payment", `{"amount":0,"category":"еда"}`},
		{"create_payment", `{"amount":1.001,"category":"еда"}`},
		{"create_payment", `{"category":"еда"}`},
		{"create_payment", `null`},
		{"create_payment", `[]`},
		{"create_payment", `{} {}`},
		{"delete_payment", `{}`},
		{"delete_payment", `{"list_index":2,"search":"такси"}`},
		{"update_payment", `{"latest":true}`},
		{"update_payment", `{"selected":true,"list_index":2,"amount":400}`},
		{"sum_payments", `{"from":"2026-11-01","to":"2026-10-01"}`},
		{"sum_payments", `{"amount":400}`},
		{"list_payments", `{"list_index":0}`},
		{"list_payments", `{"id":""}`},
		{"sum_payments", `{"filter_category":""}`},
		{"sum_payments", `{"from":""}`},
		{"get_payment_id", `{}`},
		{"ask_clarification", `{}`},
		{"ask_clarification", `{"question":" "}`},
	} {
		t.Run(tc.name+tc.args, func(t *testing.T) {
			if _, err := decodeTool(tc.name, tc.args); err == nil {
				t.Fatal("invalid arguments accepted")
			}
		})
	}
}
