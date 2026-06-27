package accountcreator

import "testing"

func TestApplyAutomationSummaryExtractsCreatedAndLinkedAccount(t *testing.T) {
	output := `[2026-06-20T22:39:00.000Z] [OK  ] === Conta criada com sucesso === {"username":"zaiconta9","email":"nova@example.test"}
[2026-06-20T22:39:01.000Z] [OK  ] Conta vinculada ao proxy GLM5.2 {"email":"nova@example.test","accountId":"acct-123","label":"Conta 26"}`
	var result Result
	applyAutomationSummary(&result, output)
	if result.Username != "zaiconta9" || result.Email != "nova@example.test" || result.AccountID != "acct-123" || result.Label != "Conta 26" {
		t.Fatalf("unexpected parsed automation summary: %+v", result)
	}
}
