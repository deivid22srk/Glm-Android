package api

import (
	"strings"
	"testing"

	"glm5.2proxy/internal/codingplan"
)

func TestCodingPlanQuotaLogIsInfoWhenStartPlanIsHealthy(t *testing.T) {
	level, event, message := codingPlanQuotaLog("Conta 3", codingplan.Result{
		QuotaError:        "billing usage-quota failed: HTTP 200 当前用户不存在coding plan",
		StartPlanVerified: true,
	})

	if level != "info" {
		t.Fatalf("expected info level, got %q", level)
	}
	if event != "coding_plan.quota_not_entitled" {
		t.Fatalf("unexpected event: %q", event)
	}
	if !strings.Contains(message, "Start Plan segue confirmado normalmente") {
		t.Fatalf("unexpected message: %q", message)
	}
}

func TestCodingPlanQuotaLogStaysWarnWhenStartPlanAlsoFailed(t *testing.T) {
	level, event, message := codingPlanQuotaLog("Conta 3", codingplan.Result{
		QuotaError:        "billing usage-quota failed: HTTP 200 当前用户不存在coding plan",
		StartPlanVerified: false,
	})

	if level != "warn" {
		t.Fatalf("expected warn level, got %q", level)
	}
	if event != "coding_plan.quota_not_entitled" {
		t.Fatalf("unexpected event: %q", event)
	}
	if !strings.Contains(message, "quota nao foi confirmada") {
		t.Fatalf("unexpected message: %q", message)
	}
}
