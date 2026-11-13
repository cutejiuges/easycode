package plugin

import (
	"easycode/internal/codec"
	"encoding/json"
	"testing"
)

// FuzzPluginManifestCannotEnableBehavior 验证合法元数据不能通过追加空贡献绕过受限导入。
func FuzzPluginManifestCannotEnableBehavior(f *testing.F) {
	for kind := uint8(0); kind < 3; kind++ {
		f.Add([]byte(`{"name":"fuzzqa","description":"Local QA","skills":["./skills","./extra"]}`), kind)
	}
	f.Add([]byte(`{"name":"fuzzqa","$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","extensions":{}}`), uint8(3))
	f.Add([]byte(`{"name":"fuzzqa","author":{"name":"QA"},"keywords":["review"],"version":"1.0.0"}`), uint8(0))
	f.Add([]byte(`{"name":"fuzzqa","name":"other"}`), uint8(1))
	f.Add([]byte{0xff, '{', '}'}, uint8(2))
	f.Fuzz(func(t *testing.T, data []byte, format uint8) {
		if len(data) > 64<<10 {
			return
		}
		kind := []string{"easycode", "claude", "codex", "agentplugins"}[format%4]
		original, err := parseManifest(data, kind)
		if err != nil {
			return
		}
		members, err := codec.ObjectMembers(data)
		if err != nil {
			t.Fatal("accepted manifest is not an object", err)
		}
		for _, field := range []string{"hooks", "mcpServers", "mcp_servers", "agents", "commands", "outputStyles", "lspServers", "apps"} {
			for _, value := range []json.RawMessage{[]byte(`{}`), []byte(`[]`), []byte(`null`)} {
				members[field] = value
				changed, err := codec.MarshalStable(members)
				if err != nil {
					t.Fatal(err)
				}
				got, err := parseManifest(changed, kind)
				if err == nil {
					t.Fatalf("unsupported behavior accepted: %s", field)
				}
				if len(changed) <= 64<<10 && got.ID != original.ID {
					t.Fatalf("unambiguous identity lost after rejected contribution: %s", field)
				}
			}
			delete(members, field)
		}
	})
}
