package secret

import (
	"fmt"
	"strings"
	"testing"

	"easycode/internal/codec"
)

func TestValueRedactsCommonRepresentations(t *testing.T) {
	value := New("top-secret")
	representations := []string{
		fmt.Sprint(value),
		fmt.Sprintf("%#v", value),
	}
	encoded, err := codec.MarshalStable(value)
	if err != nil {
		t.Fatalf("marshal secret: %v", err)
	}
	representations = append(representations, string(encoded))

	for _, representation := range representations {
		if strings.Contains(representation, "top-secret") {
			t.Fatalf("secret leaked through representation: %s", representation)
		}
	}
}
