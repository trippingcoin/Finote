package assistant

import (
	"regexp"
	"strconv"
	"strings"
)

var amountPattern = regexp.MustCompile(`^[0-9]+([.,][0-9]{1,2})?$`)

func parseAmount(text string) (float64, bool) {
	text = strings.TrimSpace(strings.TrimSuffix(strings.TrimSuffix(strings.TrimSpace(text), "₸"), "тенге"))
	if !amountPattern.MatchString(text) {
		return 0, false
	}
	amount, err := strconv.ParseFloat(strings.ReplaceAll(text, ",", "."), 64)
	return amount, err == nil
}

var ordinals = map[string]int{
	"первый": 1, "первую": 1, "первое": 1, "первая": 1,
	"второй": 2, "вторую": 2, "второе": 2, "вторая": 2,
	"третий": 3, "третью": 3, "третье": 3, "третья": 3,
	"четвёртый": 4, "четвертый": 4, "четвёртую": 4, "четвертую": 4,
	"пятый": 5, "пятую": 5, "шестой": 6, "шестую": 6, "седьмой": 7, "седьмую": 7,
	"восьмой": 8, "восьмую": 8, "девятый": 9, "девятую": 9, "десятый": 10, "десятую": 10,
}

func referenceNumber(s string) (int, bool) {
	if n, ok := ordinals[s]; ok {
		return n, true
	}
	n, err := strconv.Atoi(s)
	return n, err == nil
}

// Narrow full-message grammar: an unrecognized command never becomes a mutation.
func referenceCommand(text string) (Command, bool) {
	text = strings.ToLower(strings.Trim(strings.TrimSpace(text), ".!?"))
	switch text {
	case "а id", "id", "покажи id", "какой id", "а идентификатор":
		return Command{Action: "id", Selected: true}, true
	}
	text = strings.TrimSuffix(text, " в списке")
	fields := strings.Fields(text)
	if len(fields) < 2 {
		return Command{}, false
	}
	var action string
	switch fields[0] {
	case "покажи":
		action = "list"
	case "удали":
		action = "delete"
	case "измени":
		action = "update"
	default:
		return Command{}, false
	}
	target := fields[1]
	if target == "номер" && len(fields) > 2 {
		fields = append(fields[:1], fields[2:]...)
		target = fields[1]
	}
	target = strings.TrimPrefix(target, "№")
	i := Command{Action: action}
	if target == "его" || target == "её" || target == "ее" || target == "этот" || target == "эту" {
		i.Selected = true
	} else {
		n, ok := referenceNumber(target)
		if !ok {
			return Command{}, false
		}
		if n < 1 || n > 10 {
			return Command{Action: "clarify", Question: "Укажите номер от 1 до 10 из последнего списка."}, true
		}
		i.ListIndex = n
	}
	if action == "update" {
		if len(fields) < 4 || fields[2] != "на" {
			return Command{}, false
		}
		amount, ok := parseAmount(strings.Join(fields[3:], " "))
		if !ok {
			return Command{Action: "clarify", Question: "Укажите новую сумму, например «измени второй на 700»."}, true
		}
		i.Amount = &amount
	} else if len(fields) != 2 {
		return Command{}, false
	}
	if err := i.Validate(); err != nil {
		return Command{Action: "clarify", Question: "Проверьте сумму и номер расхода."}, true
	}
	return i, true
}
