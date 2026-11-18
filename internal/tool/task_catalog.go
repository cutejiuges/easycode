package tool

import (
	"errors"

	"easycode/internal/codec"
	"easycode/internal/domain"
)

// ServerSchemaMode 区分 function 的服务端约束，不影响本地严格解码。
type ServerSchemaMode string

const (
	ServerSchemaNotApplicable ServerSchemaMode = "not_applicable"
	ServerSchemaStrict        ServerSchemaMode = "strict"
	ServerSchemaBestEffort    ServerSchemaMode = "best_effort"
)

func (v ServerSchemaMode) Valid() bool {
	return v == ServerSchemaNotApplicable || v == ServerSchemaStrict || v == ServerSchemaBestEffort
}

func (facade Facade) ServerSchemaMode() ServerSchemaMode { return facade.serverSchema }

func serverSchemaMode(family domain.ProviderFamily, wire ProviderWireKind, capability CapabilityID) ServerSchemaMode {
	if family != domain.ProviderOpenAI || wire != WireStructured {
		return ServerSchemaNotApplicable
	}
	if capability == CapabilityTaskProgress {
		return ServerSchemaBestEffort
	}
	return ServerSchemaStrict
}

func taskFacadeProfile(family domain.ProviderFamily) (TaskProfile, error) {
	switch family {
	case domain.ProviderAnthropic:
		return TaskProfileTodoWrite, nil
	case domain.ProviderOpenAI:
		return TaskProfileUpdatePlan, nil
	default:
		return "", errors.New("unsupported task facade family")
	}
}

func decodeTaskFacadeInput(family domain.ProviderFamily, profile TaskProfile, data []byte) (TaskInput, error) {
	expected, err := taskFacadeProfile(family)
	if err != nil || profile != expected {
		return TaskInput{}, errors.New("task facade profile mismatch")
	}
	switch profile {
	case TaskProfileTodoWrite:
		return DecodeTodoWriteInput(data)
	case TaskProfileUpdatePlan:
		return DecodeUpdatePlanInput(data)
	default:
		return TaskInput{}, errors.New("invalid task facade profile")
	}
}

func taskDescription(profile TaskProfile) string {
	common := "Reports progress for multi-step work by replacing the complete task list, not merging. An empty list explicitly clears it; state persists across user turns until replaced. At most 20 items, unique exact labels, and at most one in_progress item (zero is allowed). Every provided label and activeForm must be nonempty, have no surrounding whitespace or control characters, and use at most 512 UTF-8 bytes; JSON maxLength counts characters, not bytes. Only update_plan accepts an optional explanation, limited to 2 KiB, with LF and Tab but no other controls. All text together is limited to 16 KiB; raw input and canonical snapshot are each limited to 64 KiB. Keep progress current; do not repeat the full list after success. completed is a model report, not proof that tests passed, work finished or permissions were granted."
	if profile == TaskProfileTodoWrite {
		return common + " Provide todos with content, activeForm and status for every item. activeForm describes the ongoing action. Explanation is not accepted by TodoWrite."
	}
	return common + " Provide plan items with step and status, plus optional explanation. Do not provide activeForm."
}

func buildTaskSchema(profile TaskProfile) (codec.CanonicalJSON, error) {
	if profile == TaskProfileTodoWrite {
		return codec.ParseCanonical([]byte(`{"additionalProperties":false,"properties":{"todos":{"items":{"additionalProperties":false,"properties":{"activeForm":{"description":"Nonempty ongoing-action description; at most 512 UTF-8 bytes, without surrounding whitespace or control characters.","maxLength":512,"type":"string"},"content":{"description":"Nonempty exact task label; at most 512 UTF-8 bytes, without surrounding whitespace or control characters.","maxLength":512,"type":"string"},"status":{"enum":["pending","in_progress","completed"],"type":"string"}},"required":["content","activeForm","status"],"type":"object"},"maxItems":20,"type":"array"}},"required":["todos"],"type":"object"}`), maxCatalogBytes)
	}
	if profile != TaskProfileUpdatePlan {
		return codec.CanonicalJSON{}, errors.New("invalid task schema profile")
	}
	return codec.ParseCanonical([]byte(`{"additionalProperties":false,"properties":{"explanation":{"description":"Optional explanation, at most 2 KiB UTF-8 bytes; LF and Tab are allowed, other control characters are not.","maxLength":2048,"type":"string"},"plan":{"items":{"additionalProperties":false,"properties":{"status":{"enum":["pending","in_progress","completed"],"type":"string"},"step":{"description":"Nonempty exact task label; at most 512 UTF-8 bytes, without surrounding whitespace or control characters.","maxLength":512,"type":"string"}},"required":["step","status"],"type":"object"},"maxItems":20,"type":"array"}},"required":["plan"],"type":"object"}`), maxCatalogBytes)
}

func buildTaskFacades() ([]Facade, error) {
	facades := make([]Facade, 0, 2)
	for _, family := range []domain.ProviderFamily{domain.ProviderAnthropic, domain.ProviderOpenAI} {
		profile, _ := taskFacadeProfile(family)
		schema, err := buildTaskSchema(profile)
		if err != nil {
			return nil, err
		}
		name, id := facadeIdentity(family, CapabilityTaskProgress)
		facades = append(facades, Facade{id: id, family: family, name: name, description: taskDescription(profile), capability: CapabilityTaskProgress, wireKind: WireStructured, schema: schema})
	}
	return facades, nil
}
