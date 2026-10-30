package tool

import "errors"

// ExecStreamStats 保存单个输出流的原始字节捕获统计。
type ExecStreamStats struct {
	observed int64
	retained int64
}

// NewExecStreamStats 验证单流捕获上限和字节关系。
func NewExecStreamStats(observed, retained int64) (ExecStreamStats, error) {
	stats := ExecStreamStats{observed: observed, retained: retained}
	if err := stats.Validate(); err != nil {
		return ExecStreamStats{}, err
	}
	return stats, nil
}

func (stats ExecStreamStats) ObservedBytes() int64 { return stats.observed }
func (stats ExecStreamStats) RetainedBytes() int64 { return stats.retained }
func (stats ExecStreamStats) OmittedBytes() int64  { return stats.observed - stats.retained }

// Validate 验证统计未超过捕获硬限制。
func (stats ExecStreamStats) Validate() error {
	if stats.observed < 0 || stats.retained < 0 || stats.retained > stats.observed || stats.retained > ExecCaptureBytesPerStream {
		return errors.New("invalid Exec stream statistics")
	}
	return nil
}

// ExecResultMetadata 保存无宿主路径和进程标识的命令终态元数据。
type ExecResultMetadata struct {
	workdir          string
	exitCode         int
	exitKnown        bool
	durationMillis   int64
	stdout           ExecStreamStats
	stderr           ExecStreamStats
	timedOut         bool
	cancelled        bool
	previewTruncated bool
	stdoutSaving     ArtifactSaving
	stderrSaving     ArtifactSaving
}

// NewExecResultMetadata 构造完整命令终态摘要；未知退出码必须使用零占位。
func NewExecResultMetadata(workdir string, exitCode int, exitKnown bool, durationMillis int64, stdout, stderr ExecStreamStats, timedOut, cancelled, previewTruncated bool) (ExecResultMetadata, error) {
	metadata := ExecResultMetadata{workdir: workdir, exitCode: exitCode, exitKnown: exitKnown, durationMillis: durationMillis, stdout: stdout, stderr: stderr, timedOut: timedOut, cancelled: cancelled, previewTruncated: previewTruncated}
	if err := metadata.Validate(); err != nil {
		return ExecResultMetadata{}, err
	}
	return metadata, nil
}

func (metadata ExecResultMetadata) Workdir() string { return metadata.workdir }
func (metadata ExecResultMetadata) ExitCode() (int, bool) {
	return metadata.exitCode, metadata.exitKnown
}
func (metadata ExecResultMetadata) DurationMillis() int64   { return metadata.durationMillis }
func (metadata ExecResultMetadata) Stdout() ExecStreamStats { return metadata.stdout }
func (metadata ExecResultMetadata) Stderr() ExecStreamStats { return metadata.stderr }
func (metadata ExecResultMetadata) TimedOut() bool          { return metadata.timedOut }
func (metadata ExecResultMetadata) Cancelled() bool         { return metadata.cancelled }

// WithOutputSaving 在生成最终预览前冻结两个流的保存状态。
func (metadata ExecResultMetadata) WithOutputSaving(stdout, stderr ArtifactSaving) (ExecResultMetadata, error) {
	if stdout.Validate() != nil || stderr.Validate() != nil {
		return ExecResultMetadata{}, errors.New("invalid Exec output saving")
	}
	if out, ok := stdout.Descriptor(); ok {
		if other, exists := stderr.Descriptor(); exists && out.ID() == other.ID() {
			return ExecResultMetadata{}, errors.New("duplicate Exec artifact identity")
		}
	}
	for _, pair := range []struct {
		s      ArtifactSaving
		stream ArtifactStream
		stats  ExecStreamStats
	}{{stdout, ArtifactStdout, metadata.stdout}, {stderr, ArtifactStderr, metadata.stderr}} {
		if d, ok := pair.s.Descriptor(); ok && (d.Stream() != pair.stream || d.ObservedBytes() != pair.stats.ObservedBytes()) {
			return ExecResultMetadata{}, errors.New("Exec artifact stream mismatch")
		}
	}
	metadata.stdoutSaving = stdout
	metadata.stderrSaving = stderr
	return metadata, metadata.Validate()
}

func (metadata ExecResultMetadata) StdoutSaving() ArtifactSaving {
	if metadata.stdoutSaving.status == "" {
		s, _ := NewArtifactSaving(ArtifactNotNeeded, "", ArtifactDescriptor{})
		return s
	}
	return metadata.stdoutSaving
}
func (metadata ExecResultMetadata) StderrSaving() ArtifactSaving {
	if metadata.stderrSaving.status == "" {
		s, _ := NewArtifactSaving(ArtifactNotNeeded, "", ArtifactDescriptor{})
		return s
	}
	return metadata.stderrSaving
}
func (metadata ExecResultMetadata) OutputTruncated() bool {
	return metadata.previewTruncated || metadata.stdout.OmittedBytes() > 0 || metadata.stderr.OmittedBytes() > 0
}

// Validate 验证路径、统计和互斥的取消原因。
func (metadata ExecResultMetadata) Validate() error {
	if validateSearchPath(metadata.workdir) != nil || metadata.durationMillis < 0 || metadata.exitCode < -1 || metadata.exitCode > 255 || (!metadata.exitKnown && metadata.exitCode != 0) || (metadata.timedOut && metadata.cancelled) || metadata.stdout.Validate() != nil || metadata.stderr.Validate() != nil {
		return errors.New("invalid Exec result metadata")
	}
	return nil
}

// NewExecInvocationResult 冻结命令结果及与原生调用的配对身份。
func NewExecInvocationResult(invocation ExecInvocation, status ResultStatus, code string, preview ModelPreview, metadata ExecResultMetadata) (InvocationResult, error) {
	if err := invocation.Validate(); err != nil {
		return InvocationResult{}, err
	}
	result := InvocationResult{invocationID: invocation.invocationID, providerCallID: invocation.providerCallID, capability: CapabilityExec, wireKind: WireStructured, status: status, code: code, preview: preview, execMetadata: metadata}
	return initializeResultBudget(result)
}

func (result InvocationResult) ExecMetadata() ExecResultMetadata { return result.execMetadata }

func (result InvocationResult) validateExecResult() error {
	m := result.execMetadata
	for _, saving := range []ArtifactSaving{m.StdoutSaving(), m.StderrSaving()} {
		if saving.Validate() != nil {
			return errors.New("invalid Exec saving state")
		}
		if d, ok := saving.Descriptor(); ok && d.InvocationID() != result.invocationID {
			return errors.New("Exec artifact invocation mismatch")
		}
	}
	if err := m.Validate(); err != nil {
		return err
	}
	if result.wireKind != WireStructured || result.readMetadata != (ReadResultMetadata{}) || !result.globMetadata.zero() || !result.grepMetadata.zero() || !result.patchMetadata.zero() {
		return errors.New("Exec result contains mismatched metadata")
	}
	if result.status == ResultConflict ||
		(m.timedOut && result.status != ResultOutcomeUncertain && (result.status != ResultError || result.code != "timed_out")) ||
		(result.code == "timed_out" && !m.timedOut) ||
		(result.status != ResultOutcomeUncertain && m.cancelled != (result.status == ResultCancelled)) {
		return errors.New("Exec result has inconsistent terminal state")
	}
	if result.status == ResultSuccess && (!m.exitKnown || m.exitCode < 0 || m.timedOut || m.cancelled) {
		return errors.New("successful Exec result requires a known exit code")
	}
	if result.status == ResultDenied && (m.exitKnown || m.durationMillis != 0 || m.stdout.observed != 0 || m.stderr.observed != 0) {
		return errors.New("denied Exec result contains execution metadata")
	}
	return nil
}

// NewExecErrorResult 构造没有底层错误正文的确定拒绝、取消或失败结果。
func NewExecErrorResult(invocation ExecInvocation, status ResultStatus, code string, message string) InvocationResult {
	metadata := ExecResultMetadata{workdir: invocation.input.workdir, cancelled: status == ResultCancelled, timedOut: code == "timed_out"}
	preview, err := NewModelPreview(message)
	if err == nil {
		result, resultErr := NewExecInvocationResult(invocation, status, code, preview, metadata)
		if resultErr == nil {
			return result
		}
	}
	result, _ := initializeResultBudget(InvocationResult{invocationID: invocation.invocationID, providerCallID: invocation.providerCallID, capability: CapabilityExec, wireKind: WireStructured, status: ResultError, code: "internal_error", preview: ModelPreview{text: "Exec failed: internal_error"}, execMetadata: ExecResultMetadata{workdir: invocation.input.workdir}})
	return result
}
