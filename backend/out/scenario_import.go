package out

import "main/services"

// ScenarioImport — ответ импорта Excel: сценарий со шагами, правилами и командами
// (ScenarioDetail) и import_report (api_contract.md, v4).
type ScenarioImport struct {
	ScenarioDetail
	ImportReport services.ImportReport `json:"import_report"`
}

func ScenarioImportFrom(r services.ImportResult) ScenarioImport {
	rep := r.Report
	if rep.Warnings == nil {
		rep.Warnings = []services.ImportWarning{}
	}
	if rep.MediaNameList == nil {
		rep.MediaNameList = []string{}
	}
	return ScenarioImport{ScenarioDetail: ScenarioDetailFrom(*r.Detail), ImportReport: rep}
}
