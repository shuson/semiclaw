package gateway

import "testing"

func TestToolPolicyForMode_AutomationSafeDeniesSensitiveTools(t *testing.T) {
	got := ToolPolicyForMode(ToolPolicyModeAutomationSafe)

	if got["browser"].Allowed != true {
		t.Fatal("expected browser to remain allowed")
	}
	if got["shell"].Allowed {
		t.Fatal("expected shell to be denied")
	}
	if got["python"].Allowed {
		t.Fatal("expected python to be denied")
	}
	if got["file"].Allowed {
		t.Fatal("expected file to be denied")
	}
}

func TestToolPolicyForMode_AutomationAllowAllDisablesApprovalPrompts(t *testing.T) {
	got := ToolPolicyForMode(ToolPolicyModeAutomationAllowAll)

	for name, permission := range got {
		if !permission.Allowed {
			t.Fatalf("expected %s to be allowed", name)
		}
		if permission.RequireUserApproval {
			t.Fatalf("expected %s to skip approval", name)
		}
	}
}

