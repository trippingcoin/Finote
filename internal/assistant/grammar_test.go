package assistant

import "testing"

func TestReferenceGrammar(t *testing.T) {
	for _, text := range []string{"покажи 1", "покажи первое в списке", "измени второй на 700", "а id?", "удали второй"} {
		i, ok := referenceCommand(text)
		if !ok || i.Validate() != nil {
			t.Fatalf("%s: %+v", text, i)
		}
	}
	for _, text := range []string{"кофе 400", "удали второй и первый", "покажи список"} {
		if _, ok := referenceCommand(text); ok {
			t.Fatal(text)
		}
	}
	for _, text := range []string{"400", "400,50", "700 ₸"} {
		if _, ok := parseAmount(text); !ok {
			t.Fatal(text)
		}
	}
	for _, text := range []string{"-1", "NaN", "400 кофе", "4.555"} {
		if _, ok := parseAmount(text); ok {
			t.Fatal(text)
		}
	}
}
