//go:build !page_unknown_trace

package collect

import "github.com/LoTwT/youkai/internal/platform"

// StoragePageTraceBuilt reports whether this binary includes the QA trace.
const StoragePageTraceBuilt = false

type pageUnknownTraceRing struct{}

func tracePageUnknownInput(_ *inventoryRun, _ int, _ uint64, _ platform.InputResult)            {}
func tracePageUnknownAction(_ *inventoryRun, _ string, _ int, _ uint64, _ platform.InputResult) {}
func tracePageUnknownStage(_ *inventoryRun, _ string)                                           {}
func tracePageUnknownObserve(_ *inventoryRun, _ platform.Frame, _ PageMatch, _ int, _ detailOutcome) {
}
func tracePageUnknownObserveUnmatched(_ *inventoryRun, _ platform.Frame, _ int)             {}
func tracePageUnknownReject(_ *inventoryRun, _ platform.Frame, _ PageMatch, _ int, _ error) {}
func tracePageUnknownWeak(_ *inventoryRun, _ platform.Frame, _ int, _ int)                  {}
func tracePageUnknownTerminal(_ *inventoryRun, _ platform.Frame, _ PageMatch, _ int, _ error, _ string) {
}
func tracePageUnknownTimeout(_ *inventoryRun, _ int, _ detailOutcome)                               {}
func tracePageUnknownReferenceFault(_ *inventoryRun, _ int, _ platform.Frame, _ PageMatch, _ error) {}
func tracePageUnknownFrameFault(_ *inventoryRun, _ int, _ platform.Frame, _ PageMatch, _ bool, _ error) {
}
func tracePageUnknownWaitFault(_ *inventoryRun, _ int, _ platform.Frame, _ PageMatch, _ error) {}
func tracePageUnknownWaitTimeout(_ *inventoryRun, _ int, _ detailOutcome, _ error)             {}

func tracePageUnknownTargetBadgeFault(_ *inventoryRun, _ platform.Frame, _ PageMatch, _ int, _ error) {
}
