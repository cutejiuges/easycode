package codec

import "testing"

func TestStrictObjectContract(t *testing.T) {
	type child struct {
		Value string `json:"value"`
	}
	type input struct {
		Name  string `json:"name"`
		Limit int    `json:"limit"`
		Child child  `json:"child"`
	}
	for _, data := range []string{
		`{"name":"x","name":"y"}`, `{"name":"x","child":{"value":"a","value":"b"}}`,
		`{"name":"x","child":{"Value":"a"}}`, `{"Name":"x"}`, `{"name":null}`,
		`{"name":"x","limit":null}`, `{"name":"x"} {}`, `{"name":"\ud800"}`, `{"name":"\udc00"}`,
		`{"name":"\ud800x"}`, `{}`, `{"name":"x","unknown":1}`, string([]byte{'{', '"', 'n', 'a', 'm', 'e', '"', ':', '"', 0xff, '"', '}'}),
	} {
		t.Run(data, func(t *testing.T) {
			value := input{Limit: 42}
			if err := DecodeObject([]byte(data), &value, "name"); err == nil {
				t.Fatal("accepted ambiguous input")
			}
		})
	}
	for _, data := range []string{`{"name":""}`, `{"name":"\ud83d\ude00"}`, `{"name":"\\ud800"}`, `{"name":"x","child":{"value":"ok"}}`} {
		value := input{Limit: 42}
		if err := DecodeObject([]byte(data), &value, "name"); err != nil {
			t.Fatal(err)
		}
		if value.Limit != 42 {
			t.Fatal("omitted default changed")
		}
	}
}

func TestRecordNestedPresenceAndNull(t *testing.T) {
	type nested struct {
		Enabled bool     `json:"enabled"`
		Label   string   `json:"label,omitempty"`
		Entries []string `json:"entries"`
	}
	type record struct {
		Value nested `json:"value"`
	}
	for _, data := range []string{`{}`, `{"value":null}`, `{"value":{}}`, `{"value":{"enabled":null,"entries":[]}}`, `{"value":{"enabled":false,"label":null,"entries":[]}}`, `{"value":{"enabled":false,"entries":[null]}}`} {
		var target record
		if err := UnmarshalRecord([]byte(data), &target); err == nil {
			t.Fatalf("accepted %s", data)
		}
	}
	for _, data := range []string{`{"value":{"enabled":false,"entries":[]}}`, `{"value":{"enabled":false,"entries":null}}`} {
		var target record
		if err := UnmarshalRecord([]byte(data), &target); err != nil {
			t.Fatalf("valid current shape %s: %v", data, err)
		}
	}
}
