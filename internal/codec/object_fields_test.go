package codec

import "testing"

func TestRequiredObjectFieldsRejectLossyShapes(t *testing.T) {
	for _, data := range []string{`null`, `[]`, `{}`, `{"x":null}`, `{"x":1,"x":2}`, `{"x":1,"\u0078":2}`, `{"x":1}{}`} {
		if ValidateObjectFields([]byte(data), "x") == nil {
			t.Fatalf("接受非法字段 %s", data)
		}
	}
	if err := ValidateObjectFields([]byte(`{"x":"","nested":{"value":0}}`), "x"); err != nil {
		t.Fatal(err)
	}
}
