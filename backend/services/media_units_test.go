package services

import (
	"testing"

	"main/domain"
)

// Команды и правила сценария — тоже места использования медиатеки: иначе PDF для /food
// выглядел «неиспользуемым» и его можно было удалить.
func TestScenarioMediaUnits(t *testing.T) {
	cmds := domain.CommandList{{Name: "/Food", Actions: []domain.Action{{Type: "send_file", Media: "Где пообедать"}}}}
	rules := domain.RuleList{{Kind: "survey_reminder", Media: domain.MediaRefList{{Name: "Изображение 12"}}}}
	units := scenarioMediaUnits(cmds, rules)
	if len(units) != 2 {
		t.Fatalf("мест %d, ждали 2", len(units))
	}
	if units[0].key != "cmd:/food" || len(units[0].names) != 1 || units[0].names[0] != "Где пообедать" {
		t.Fatalf("команда: %+v", units[0])
	}
	if len(units[1].refs) != 1 || units[1].refs[0].Name != "Изображение 12" {
		t.Fatalf("правило: %+v", units[1])
	}
}
