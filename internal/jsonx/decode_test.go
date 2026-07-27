package jsonx

import "testing"

type numericEnvelope struct {
	Context map[string]map[string]any `json:"context"`
	Payload map[string]any            `json:"payload"`
}

func TestDecodePreservesIntegralAndFractionalNumbers(t *testing.T) {
	var value numericEnvelope
	if err := Decode([]byte(`{
		"context":{"Counter":{"value":42,"ratio":1.5}},
		"payload":{"nested":[1,2.25,{"count":3}]}
	}`), &value); err != nil {
		t.Fatal(err)
	}
	if got, ok := value.Context["Counter"]["value"].(int); !ok || got != 42 {
		t.Fatalf("integer = %#v (%T), want int(42)", value.Context["Counter"]["value"], value.Context["Counter"]["value"])
	}
	if got, ok := value.Context["Counter"]["ratio"].(float64); !ok || got != 1.5 {
		t.Fatalf("fraction = %#v (%T), want float64(1.5)", value.Context["Counter"]["ratio"], value.Context["Counter"]["ratio"])
	}
	nested := value.Payload["nested"].([]any)
	if _, ok := nested[0].(int); !ok {
		t.Fatalf("nested integer type = %T, want int", nested[0])
	}
	if _, ok := nested[1].(float64); !ok {
		t.Fatalf("nested fraction type = %T, want float64", nested[1])
	}
	object := nested[2].(map[string]any)
	if _, ok := object["count"].(int); !ok {
		t.Fatalf("nested object integer type = %T, want int", object["count"])
	}
}
