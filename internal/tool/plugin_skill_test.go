package tool

import (
	"strings"
	"testing"
)

func TestQualifiedSkillInputCurrentContract(t *testing.T) {
	name := strings.Repeat("a", 64) + ":" + strings.Repeat("b", 64)
	v, err := DecodeSkillInput([]byte(`{"skill":"` + name + `","args":"diff"}`))
	if err != nil || v.Name() != name || v.Arguments() != "diff" {
		t.Fatalf("qualified input: %v", err)
	}
	for _, data := range []string{`{"skill":"qa:bad:leaf"}`, `{"skill":"qa:review","resource":"../bad"}`, `{"skill":"qa:review","args":"","resource":"references/check.md"}`, `{"skill":"qa:review","extra":true}`} {
		if _, err := DecodeSkillInput([]byte(data)); err == nil {
			t.Fatalf("accepted %s", data)
		}
	}
}
