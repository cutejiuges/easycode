package tool

import (
	"encoding/binary"
	"errors"
)

// PatchFingerprint 对typed input、基线证据和final摘要进行有长度边界的稳定编码。
// 调用/批准ID、时间、绝对cwd以及UI状态不参与fingerprint。
func PatchFingerprint(input PatchInput, evidence PatchBaseEvidence, final []byte) (CandidateFingerprint, error) {
	if input.Validate() != nil || evidence.Validate() != nil {
		return "", errors.New("invalid patch fingerprint input")
	}
	canonical := make([]byte, 0, MaxPatchInputBytes+256)
	field := func(text string) {
		canonical = binary.BigEndian.AppendUint64(canonical, uint64(len(text)))
		canonical = append(canonical, text...)
	}
	field(string(CapabilityPatch))
	field(input.filePath)
	canonical = binary.BigEndian.AppendUint64(canonical, uint64(len(input.replacements)))
	for _, replacement := range input.replacements {
		field(replacement.oldText)
		field(replacement.newText)
		if replacement.replaceAll {
			canonical = append(canonical, 1)
		} else {
			canonical = append(canonical, 0)
		}
		if replacement.lineBased {
			canonical = append(canonical, 1)
		} else {
			canonical = append(canonical, 0)
		}
	}
	field(string(evidence.contentDigest))
	canonical = binary.BigEndian.AppendUint64(canonical, uint64(evidence.size))
	canonical = binary.BigEndian.AppendUint32(canonical, evidence.mode)
	canonical = binary.BigEndian.AppendUint64(canonical, evidence.device)
	canonical = binary.BigEndian.AppendUint64(canonical, evidence.inode)
	field(string(NewContentDigest(final)))
	return NewCandidateFingerprint(canonical), nil
}
