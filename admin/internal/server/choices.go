package server

import "strings"

type option struct {
	Value    string
	Label    string
	Selected bool
}

func modelOptions(current string) []option {
	return withCurrent([]option{
		{Value: "", Label: "По умолчанию — Grok"},
		{Value: "grok", Label: "Grok"},
		{Value: "claude-sonnet", Label: "Claude"},
		{Value: "gpt", Label: "GPT"},
	}, current)
}

func periodOptions(current string) []option {
	if strings.TrimSpace(current) == "" {
		current = "day"
	}
	return withCurrent([]option{
		{Value: "day", Label: "Сутки"},
		{Value: "week", Label: "Неделя"},
		{Value: "month", Label: "Месяц"},
		{Value: "12h", Label: "12 часов"},
	}, current)
}

func withCurrent(items []option, current string) []option {
	found := false
	for i := range items {
		if items[i].Value == current {
			items[i].Selected = true
			found = true
		}
	}
	if !found {
		label := current
		if label == "" {
			label = "Не задана"
		}
		items = append(items, option{Value: current, Label: label, Selected: true})
	}
	return items
}
