package app

const transcriptRowsPerBlock = 64

// transcriptRows 保留恢复时的有序前缀，实时发布只扩充末尾有界块。
type transcriptRows[T any] struct {
	restored []T
	live     map[int][]T
	count    int
}

func (rows *transcriptRows[T]) len() int {
	return len(rows.restored) + rows.count
}

func (rows *transcriptRows[T]) at(index int) *T {
	if index < len(rows.restored) {
		return &rows.restored[index]
	}
	index -= len(rows.restored)
	return &rows.live[index/transcriptRowsPerBlock][index%transcriptRowsPerBlock]
}

func (rows *transcriptRows[T]) append(nodes []T) {
	if len(nodes) == 0 {
		return
	}
	if rows.live == nil {
		rows.live = make(map[int][]T)
	}
	for len(nodes) > 0 {
		block, offset := rows.count/transcriptRowsPerBlock, rows.count%transcriptRowsPerBlock
		size := min(len(nodes), transcriptRowsPerBlock-offset)
		rows.live[block] = append(rows.live[block], nodes[:size]...)
		rows.count += size
		nodes = nodes[size:]
	}
}
