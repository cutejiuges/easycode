package tool

import (
	"easycode/internal/domain"
	"strings"
	"testing"
)

func skillTestInvocation(t *testing.T) SkillInvocation {
	t.Helper()
	i, err := NewSkillLoadInput("demo", "args")
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewSkillReadyCall("call-skill", i)
	if err != nil {
		t.Fatal(err)
	}
	v, err := NewSkillInvocation("01900000-0000-7000-8000-000000000001", c)
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func TestSkillStrictInput(t *testing.T) {
	for _, text := range []string{`{"skill":"demo","args":"","resource":"ref.md"}`, `{"skill":"demo","resource":"../secret"}`, `{"skill":"demo","resource":""}`, `{"skill":"demo","extra":true}`, `{"skill":"demo","skill":"other"}`, `{"skill":null}`, `{"skill":"demo","args":null}`, `{"skill":"demo","resource":null}`} {
		if _, err := DecodeSkillInput([]byte(text)); err == nil {
			t.Errorf("accepted %s", text)
		}
	}
	for _, text := range []string{`{"skill":"demo"}`, `{"skill":"demo","args":"a b"}`, `{"skill":"demo","resource":"references/ref.md"}`} {
		if _, err := DecodeSkillInput([]byte(text)); err != nil {
			t.Errorf("rejected %s: %v", text, err)
		}
	}
}
func TestSkillWholeResultBudgetAndIdentity(t *testing.T) {
	v := skillTestInvocation(t)
	source, _ := domain.NewSkillSource("project", ".", "easycode")
	body := strings.Repeat("x", 100<<10)
	a, err := domain.NewSkillAttachment(source, "demo", domain.SkillDigest("source"), "args", body)
	if err != nil {
		t.Fatal(err)
	}
	p, _ := NewModelPreview(body)
	r, err := NewSkillInvocationResult(v, ResultSuccess, "ok", p, a)
	if err != nil {
		t.Fatal(err)
	}
	budget := NewResultBudget()
	if err := budget.BeginSample(5); err != nil {
		t.Fatal(err)
	}
	for n := 0; n < 5; n++ {
		allowance, err := budget.Allowance(CapabilitySkill)
		if err != nil {
			t.Fatal(err)
		}
		final, err := FinalizeResult(r, allowance, BudgetPreserved)
		if err != nil {
			t.Fatal(err)
		}
		if final.Status() == ResultSuccess && final.Preview().Text() != body {
			t.Fatal("partial skill body")
		}
		if err := budget.Commit(CapabilitySkill, final.Budget()); err != nil {
			t.Fatal(err)
		}
	}
	rejected, err := FinalizeResult(r, SafetyResultBytes, BudgetPreserved)
	if err != nil || rejected.Status() != ResultError || rejected.Code() != "result_budget_exceeded" {
		t.Fatalf("budget: %v", err)
	}
	if _, ok := rejected.SkillAttachment(); ok {
		t.Fatal("rejected attachment retained")
	}
	a.Name = "other"
	if _, err := NewSkillInvocationResult(v, ResultSuccess, "ok", p, a); err == nil {
		t.Fatal("mismatched name")
	}
}
