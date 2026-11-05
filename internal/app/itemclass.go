package app

import (
	"github.com/LoTwT/youkai/internal/game/zzz"
	"github.com/LoTwT/youkai/internal/record"
)

// ItemClass is one of the four mutually exclusive display classes of a
// captured item. The order in which they are decided is fixed: processing
// outcome, validation, quality, warning. An item lands in the first class
// it matches, so a warning item that also has a data gap is pending
// review, not "warning".
//
// The four classes partition a record's captured items: their counts add
// up to the captured count, and warning + passed is exactly the set the
// usable-data export includes.
type ItemClass string

const (
	// ItemClassPendingBackfill is an item whose processing outcome failed
	// or that has not been processed yet (queued, running or unprocessed).
	// Failed and unprocessed items stay distinguishable through
	// record.Status and UnprocessedReason, but both need a person.
	ItemClassPendingBackfill ItemClass = "pendingBackfill"
	// ItemClassPendingReview is a produced item a person still has to
	// settle: needsReview quality, failed or unavailable validation, or a
	// payload the export cannot use.
	ItemClassPendingReview ItemClass = "pendingReview"
	// ItemClassWarning is exportable and carries at least one warning.
	// A warning does not block automatic acceptance in v2, so this class
	// may hold an auto-accepted item; it is still not "passed".
	ItemClassWarning ItemClass = "warning"
	// ItemClassPassed is exportable and warning-free: automatically
	// accepted or manually confirmed.
	ItemClassPassed ItemClass = "passed"
)

// ItemClassCounts is the four-class partition of a record's captured
// items. PendingBackfill + PendingReview + Warning + Passed equals the
// captured count, and Warning + Passed equals the export's included
// count.
type ItemClassCounts struct {
	PendingBackfill int64
	PendingReview   int64
	Warning         int64
	Passed          int64
}

// Pending is how many items a person still has to handle; it is the
// design's "待处理" total (backfill plus review).
func (c ItemClassCounts) Pending() int64 {
	return c.PendingBackfill + c.PendingReview
}

// Exportable is how many items the usable-data export includes.
func (c ItemClassCounts) Exportable() int64 {
	return c.Warning + c.Passed
}

// Total is the captured count the four classes partition.
func (c ItemClassCounts) Total() int64 {
	return c.PendingBackfill + c.PendingReview + c.Warning + c.Passed
}

func (c *ItemClassCounts) add(class ItemClass) {
	switch class {
	case ItemClassPendingBackfill:
		c.PendingBackfill++
	case ItemClassPendingReview:
		c.PendingReview++
	case ItemClassWarning:
		c.Warning++
	case ItemClassPassed:
		c.Passed++
	}
}

// ClassifyItem returns the display class of one captured item using the
// fixed order: processing outcome, validation, quality, warning.
//
// The validation and quality steps reuse the export decision
// (usableItem): an item is only usable after the persisted issues and the
// current validation are error-free and its quality is settled. The
// quality step is additionally named through itemPendingReview, so a
// needs-review item cannot become "warning" or "passed" if the export
// rules ever widen. Warnings are checked last, on the same issue lists the
// export carries.
func ClassifyItem(item record.ItemRecord) ItemClass {
	exported, usable := usableItem(&item)
	return classifyWithExport(&item, exported, usable)
}

// ClassifyItems counts the four classes of a record's items in one pass.
func ClassifyItems(items []record.ItemRecord) ItemClassCounts {
	var counts ItemClassCounts
	for i := range items {
		counts.add(ClassifyItem(items[i]))
	}
	return counts
}

// classifyWithExport applies the fixed order to an item whose export
// decision is already known, so the export build does not validate items
// twice. It must stay the same decision as ClassifyItem.
func classifyWithExport(item *record.ItemRecord, exported ExportItem, usable bool) ItemClass {
	// 1. Processing outcome: a failed or not-yet-processed observation has
	// no data to validate. Failed and unprocessed items both need a person
	// to fill them in.
	if item.Status != record.StatusProduced {
		return ItemClassPendingBackfill
	}
	// 2. Validation and 3. Quality: a validation failure, an unresolved
	// quality or a payload the export refuses all leave the item with a
	// person. itemPendingReview is only consulted for otherwise usable
	// items, so the common refusal path stays cheap.
	if !usable || itemPendingReview(*item) {
		return ItemClassPendingReview
	}
	// 4. Warning: only a usable item can be "warning but exportable".
	if itemHasWarning(exported) {
		return ItemClassWarning
	}
	return ItemClassPassed
}

// itemHasWarning reports whether an exportable item carries a warning in
// its persisted issues or its current validation. DataCheck folds the
// Warnings slice into Validation.Issues, so these two lists are the same
// rule without validating the payload a second time.
func itemHasWarning(item ExportItem) bool {
	return hasWarningIssue(item.Issues) || hasWarningIssue(item.Validation.Issues)
}

func hasWarningIssue(issues []zzz.Issue) bool {
	for _, issue := range issues {
		if issue.Severity == zzz.SeverityWarning {
			return true
		}
	}
	return false
}
