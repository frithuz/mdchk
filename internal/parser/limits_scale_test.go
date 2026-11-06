package parser

import "testing"

func TestContentScalingGrowsAllowancesOnlyWhenEnabled(t *testing.T) {
	t.Parallel()
	limits := DefaultLimits()
	limits.WorkUnits, limits.TotalNodes, limits.DocumentNodes = 1000, 100, 100

	fixed, err := NewOperationBudget(limits)
	if err != nil {
		t.Fatal(err)
	}
	fixed.GrantContent(1 << 20)
	if err := fixed.ChargeWork(1001); err == nil {
		t.Fatal("unscaled budget grew with content")
	}

	scaled, err := NewOperationBudget(limits)
	if err != nil {
		t.Fatal(err)
	}
	scaled.ScaleWithContent(10, 0)
	if err := scaled.ChargeWork(1000); err != nil {
		t.Fatalf("floor must stay available: %v", err)
	}
	scaled.GrantContent(100)
	if err := scaled.ChargeWork(1000); err != nil {
		t.Fatalf("granted content must raise the work allowance: %v", err)
	}
	if err := scaled.ChargeNodes(101); err == nil {
		t.Fatal("zero node rate must keep total-nodes fixed")
	}

	exhausted, _ := NewOperationBudget(limits)
	exhausted.ScaleWithContent(10, 10)
	if err := exhausted.ChargeWork(1001); err == nil {
		t.Fatal("charge above the floor without content must fail")
	}
	exhausted.GrantContent(1 << 20)
	if err := exhausted.ChargeWork(1); err == nil {
		t.Fatal("exhaustion is sticky")
	}
}

func TestRenewedBudgetKeepsScalingAndGrantedContent(t *testing.T) {
	t.Parallel()
	limits := DefaultLimits()
	limits.WorkUnits, limits.TotalNodes, limits.DocumentNodes = 1000, 100, 100
	b, _ := NewOperationBudget(limits)
	b.ScaleWithContent(10, 1)
	b.GrantContent(100)
	fresh, err := b.Renewed()
	if err != nil {
		t.Fatal(err)
	}
	if err := fresh.ChargeWork(2000); err != nil {
		t.Fatalf("renewed budget lost its content grant: %v", err)
	}
	if err := fresh.ChargeNodes(200); err != nil {
		t.Fatalf("renewed budget lost node scaling: %v", err)
	}
}
