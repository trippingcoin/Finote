package llm

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"finote/internal/assistant"
)

type tool struct {
	name, action, description string
	fields, required          []string
}

var selectors = []string{"id", "latest", "search", "filter_category", "from", "to", "list_index", "selected"}
var changes = []string{"category", "amount", "comment", "spent_at"}
var tools = []tool{
	{"create_payment", "create", "Добавить расход. Если сумма неизвестна, пропусти amount: сервер попросит её уточнить.", changes, []string{"category"}},
	{"list_payments", "list", "Показать расходы или конкретную запись из сохранённого списка.", selectors, nil},
	{"sum_payments", "sum", "Посчитать сумму и количество расходов по фильтру.", []string{"search", "filter_category", "from", "to"}, nil},
	{"update_payment", "update", "Изменить одну трату. Передай критерий выбора и только изменяемые поля. При нескольких совпадениях сервер уточнит выбор.", append(append([]string{}, selectors...), changes...), nil},
	{"delete_payment", "delete", "Удалить одну трату по критерию. При нескольких совпадениях сервер уточнит выбор.", selectors, nil},
	{"get_payment_id", "id", "Показать ID явно указанной или ранее выбранной записи.", []string{"id", "list_index", "selected"}, nil},
	{"ask_clarification", "clarify", "Задать вопрос, когда действие неясно или запрошены несколько операций либо чужие расходы.", []string{"question"}, []string{"question"}},
}

func property(name string) map[string]any {
	switch name {
	case "category", "filter_category":
		return map[string]any{"type": "string", "enum": []string{"еда", "транспорт", "жильё", "развлечения", "здоровье", "покупки", "другое"}}
	case "amount":
		return map[string]any{"type": "number", "minimum": 0.01, "exclusiveMaximum": 1e12, "description": "Сумма в тенге, не более двух знаков после запятой"}
	case "list_index":
		return map[string]any{"type": "integer", "minimum": 1, "maximum": 10, "description": "Номер из последнего списка; несовместим с другими критериями"}
	case "selected":
		return map[string]any{"type": "boolean", "description": "Ссылка на выбранную запись; несовместима с другими критериями"}
	case "latest":
		return map[string]any{"type": "boolean", "description": "Только если пользователь явно сказал последняя/последнее"}
	case "comment":
		return map[string]any{"type": "string", "maxLength": 500}
	case "from", "to":
		return map[string]any{"type": "string", "format": "date"}
	case "spent_at":
		return map[string]any{"type": "string", "format": "date-time"}
	case "id":
		return map[string]any{"type": "string", "format": "uuid", "description": "Только явно указанный пользователем UUID"}
	case "search":
		return map[string]any{"type": "string", "maxLength": 500, "description": "Часть комментария существующего расхода"}
	default:
		return map[string]any{"type": "string"}
	}
}

func toolDefinitions() []any {
	definitions := make([]any, 0, len(tools))
	for _, t := range tools {
		properties := map[string]any{}
		for _, field := range t.fields {
			properties[field] = property(field)
		}
		required := t.required
		if required == nil {
			required = []string{}
		}
		definitions = append(definitions, map[string]any{"type": "function", "function": map[string]any{
			"name": t.name, "description": t.description,
			"parameters": map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false},
		}})
	}
	return definitions
}

func decodeTool(name, arguments string) (assistant.Command, error) {
	var spec *tool
	for n := range tools {
		if tools[n].name == name {
			spec = &tools[n]
			break
		}
	}
	if spec == nil {
		return assistant.Command{}, errors.New("unknown expense tool")
	}
	// Enforce the selected tool's schema locally as well as in the prompt.
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(arguments), &fields); err != nil || fields == nil {
		return assistant.Command{}, errors.New("tool arguments must be one JSON object")
	}
	for key, value := range fields {
		allowed := false
		for _, f := range spec.fields {
			if f == key {
				allowed = true
				break
			}
		}
		if !allowed || strings.TrimSpace(string(value)) == "null" {
			return assistant.Command{}, fmt.Errorf("invalid field for %s: %s", name, key)
		}
	}
	for _, key := range spec.required {
		if _, ok := fields[key]; !ok {
			return assistant.Command{}, fmt.Errorf("missing tool argument: %s", key)
		}
	}
	var command assistant.Command
	decoder := json.NewDecoder(strings.NewReader(arguments))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&command); err != nil {
		return command, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return command, errors.New("extra JSON in tool arguments")
	}
	// Optional selector fields are absent when unused; explicit empty values
	// must not silently turn a targeted operation into an unfiltered query.
	if _, ok := fields["list_index"]; ok && command.ListIndex < 1 {
		return command, errors.New("invalid list index")
	}
	for _, field := range []struct{ name, value string }{
		{"id", command.ID}, {"from", command.From}, {"to", command.To}, {"filter_category", command.FilterCategory},
	} {
		if _, ok := fields[field.name]; ok && field.value == "" {
			return command, fmt.Errorf("empty tool argument: %s", field.name)
		}
	}
	command.Action = spec.action
	if command.Action == "create" && command.Amount == nil {
		command.Action = "draft_create"
	}
	if command.Action == "clarify" && strings.TrimSpace(command.Question) == "" {
		return command, errors.New("empty clarification")
	}
	return command, command.Validate()
}
