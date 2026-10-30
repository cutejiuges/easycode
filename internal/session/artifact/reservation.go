package artifact

import "easycode/internal/tool"

// reservationSpec 是唯一当前保存预留格式，额度由来源集合决定，不信任磁盘数字。
type reservationSpec struct {
	wire    string
	bytes   int64
	sources int
}

func commandReservation() reservationSpec {
	return reservationSpec{"{\"sources\":[\"stdout\",\"stderr\"],\"bytes\":33554432}\n", tool.ArtifactInvocationBytes, 2}
}

func searchReservation() reservationSpec {
	return reservationSpec{"{\"sources\":[\"search_result\"],\"bytes\":16777216}\n", tool.ArtifactStreamBytes, 1}
}

func decodeReservation(wire string) (reservationSpec, bool) {
	for _, spec := range []reservationSpec{commandReservation(), searchReservation()} {
		if wire == spec.wire {
			return spec, true
		}
	}
	return reservationSpec{}, false
}
