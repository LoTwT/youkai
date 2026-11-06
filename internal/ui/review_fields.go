package ui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/LoTwT/youkai/internal/app"
	"github.com/LoTwT/youkai/internal/game/zzz"
	"github.com/LoTwT/youkai/internal/record"
)

// reviewProblemKind separates the three ways a field can need attention: a
// present but malformed value, a required value still missing, and an
// optional field whose warning stays visible without blocking anything.
type reviewProblemKind int

const (
	reviewProblemBlocking reviewProblemKind = iota
	reviewProblemGap
	reviewProblemWarning
)

// reviewProblem is one problem shown under its form field, and the same
// problem the item list's category classification counts.
type reviewProblem struct {
	Field    string
	Code     string
	Message  string
	Severity zzz.Severity
	Kind     reviewProblemKind
}

// Blocks reports whether the problem stops saving, confirming or exporting.
func (p reviewProblem) Blocks() bool { return p.Kind != reviewProblemWarning }

// canonicalFormField maps a payload field path to the form field it
// belongs to. An empty result means the problem is about the whole item.
func canonicalFormField(field string) FormField {
	switch field {
	case "setId":
		return FormFieldSetID
	case "rarity":
		return FormFieldRarity
	case "slot":
		return FormFieldSlot
	case "level":
		return FormFieldLevel
	case "mainStat":
		return FormFieldMainStat
	case "substats":
		return FormFieldSubstats
	case "substats[0]":
		return FormFieldSubstat1
	case "substats[1]":
		return FormFieldSubstat2
	case "substats[2]":
		return FormFieldSubstat3
	case "substats[3]":
		return FormFieldSubstat4
	case "managementState":
		return FormFieldManagementState
	case "equipped", "equipped.agentId":
		return FormFieldEquipped
	default:
		return ""
	}
}

// formFieldPath is the canonical payload path a form field reports.
func formFieldPath(field FormField) string {
	if field == FormFieldSubstats {
		return "substats"
	}
	return string(field)
}

// formFieldLabel names one form field in the interface's language.
func formFieldLabel(field FormField) string {
	switch field {
	case FormFieldSetID:
		return "套装"
	case FormFieldRarity:
		return "稀有度"
	case FormFieldSlot:
		return "槽位"
	case FormFieldLevel:
		return "等级"
	case FormFieldMainStat:
		return "主词条"
	case FormFieldSubstats:
		return "副词条"
	case FormFieldSubstat1:
		return "副词条 1"
	case FormFieldSubstat2:
		return "副词条 2"
	case FormFieldSubstat3:
		return "副词条 3"
	case FormFieldSubstat4:
		return "副词条 4"
	case FormFieldManagementState:
		return "管理状态"
	case FormFieldEquipped:
		return "装备"
	default:
		return "整件"
	}
}

// formFieldRank orders fields by the form's reading order; unknown fields
// sort after the known ones.
func formFieldRank(field FormField) int {
	for i, candidate := range reviewFieldOrder {
		if candidate == field {
			return i
		}
	}
	return len(reviewFieldOrder)
}

// reviewProblems collects the problems of one item from the open editor
// when it belongs to the row, else from the persisted payload and a fresh
// validation. A completed item with no payload yet reports every required
// field as a gap, which is exactly the backfill form's starting state.
func (m *Model) reviewProblems(row *Row) []reviewProblem {
	if row == nil {
		return nil
	}
	editing := m.edit != nil && m.rec != nil &&
		m.edit.recordID == m.rec.ID && m.edit.seq == row.Seq
	var problems []reviewProblem
	// Problems are deduplicated by (field, code), not by their prose: a
	// persisted old message and the current validation can describe the
	// same issue with different wording. A problem without a code keeps
	// its message as the identity.
	seen := make(map[string]struct{})
	add := func(field, code, message string, severity zzz.Severity, kind reviewProblemKind) {
		message = reviewIssueMessage(code, message)
		if message == "" {
			return
		}
		identity := code
		if identity == "" {
			identity = message
		}
		key := field + "\x00" + identity
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		problems = append(problems, reviewProblem{
			Field: field, Code: code, Message: message, Severity: severity, Kind: kind,
		})
	}
	if editing {
		for _, problem := range m.edit.fieldErrors {
			add(problem.Field, "", problem.Message, zzz.SeverityError, reviewProblemBlocking)
		}
		for _, issue := range m.edit.check.Blocking {
			add(issue.Field, issue.Code, issue.Message, zzz.SeverityError, reviewProblemBlocking)
		}
		for _, gap := range m.edit.check.Gaps {
			add(gap.Field, gap.Code, gap.Message, zzz.SeverityWarning, reviewProblemGap)
		}
		for _, warning := range m.edit.check.Warnings {
			add(warning.Field, warning.Code, warning.Message, zzz.SeverityWarning, reviewProblemWarning)
		}
		if row.Structured != nil {
			for _, issue := range row.Structured.Issues {
				kind := reviewProblemWarning
				if issue.Severity == zzz.SeverityError {
					kind = reviewProblemBlocking
				}
				add(issue.Field, issue.Code, issue.Message, issue.Severity, kind)
			}
		}
	} else if row.Structured != nil {
		check := app.CheckDiscData(row.Structured.Data)
		for _, issue := range check.Blocking {
			add(issue.Field, issue.Code, issue.Message, zzz.SeverityError, reviewProblemBlocking)
		}
		for _, gap := range check.Gaps {
			add(gap.Field, gap.Code, gap.Message, zzz.SeverityWarning, reviewProblemGap)
		}
		for _, warning := range check.Warnings {
			add(warning.Field, warning.Code, warning.Message, zzz.SeverityWarning, reviewProblemWarning)
		}
		for _, issue := range row.Structured.Issues {
			kind := reviewProblemWarning
			if issue.Severity == zzz.SeverityError {
				kind = reviewProblemBlocking
			}
			add(issue.Field, issue.Code, issue.Message, issue.Severity, kind)
		}
	} else {
		check := app.CheckDiscData(zzz.DiscData{})
		for _, gap := range check.Gaps {
			add(gap.Field, gap.Code, gap.Message, zzz.SeverityWarning, reviewProblemGap)
		}
	}
	sort.SliceStable(problems, func(i, j int) bool {
		a, b := problems[i], problems[j]
		af, bf := canonicalFormField(a.Field), canonicalFormField(b.Field)
		if ar, br := formFieldRank(af), formFieldRank(bf); ar != br {
			return ar < br
		}
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		return a.Field < b.Field
	})
	return problems
}

// reviewIssueMessage generates the current interface text for codes whose
// persisted message may be stale. Unmapped codes keep the record's own
// prose.
func reviewIssueMessage(code, message string) string {
	switch code {
	case zzz.CodeRarityUnverified:
		return "本次捕获没有已验证的 S 稀有度，稀有度保持未读取"
	}
	return message
}

// isSubstatField reports one of the four row fields inside the group.
func isSubstatField(field FormField) bool {
	switch field {
	case FormFieldSubstat1, FormFieldSubstat2, FormFieldSubstat3, FormFieldSubstat4:
		return true
	}
	return false
}

// substatFormField names the row field for a zero-based substat index.
func substatFormField(index int) FormField {
	switch index {
	case 0:
		return FormFieldSubstat1
	case 1:
		return FormFieldSubstat2
	case 2:
		return FormFieldSubstat3
	case 3:
		return FormFieldSubstat4
	default:
		return FormFieldSubstats
	}
}

// reviewFieldShows reports whether a built field should show the selected
// highlight. The substat group contains the four individually selected
// rows, so any of them highlights the group.
func reviewFieldShows(field, selected FormField) bool {
	return field == selected || (field == FormFieldSubstats && isSubstatField(selected))
}

// problemMatchesField reports whether a problem is shown under field. The
// substat group owns the problems of each substat row, because the form
// edits all four rows in one group.
func problemMatchesField(problem reviewProblem, field FormField) bool {
	canonical := canonicalFormField(problem.Field)
	if field == FormFieldSubstats {
		switch canonical {
		case FormFieldSubstats, FormFieldSubstat1, FormFieldSubstat2,
			FormFieldSubstat3, FormFieldSubstat4:
			return true
		}
	}
	return problem.Field == formFieldPath(field) || canonical == field
}

// reviewProblemKindFor is the worst problem kind of one field.
func reviewProblemKindFor(problems []reviewProblem, field FormField) (reviewProblemKind, bool) {
	worst := reviewProblemWarning
	found := false
	for _, problem := range problems {
		if !problemMatchesField(problem, field) {
			continue
		}
		if !found || problem.Kind < worst {
			worst = problem.Kind
			found = true
		}
	}
	return worst, found
}

// problemFields lists the fields that carry a problem, in form order, each
// once.
func problemFields(problems []reviewProblem) []FormField {
	var fields []FormField
	seen := make(map[FormField]bool)
	for _, problem := range problems {
		field := canonicalFormField(problem.Field)
		if field == "" {
			continue
		}
		// A substat row problem belongs to the whole substat group: the
		// form edits all four rows in one block.
		switch field {
		case FormFieldSubstat1, FormFieldSubstat2, FormFieldSubstat3, FormFieldSubstat4:
			field = FormFieldSubstats
		}
		if seen[field] {
			continue
		}
		seen[field] = true
		fields = append(fields, field)
	}
	sort.SliceStable(fields, func(i, j int) bool {
		return formFieldRank(fields[i]) < formFieldRank(fields[j])
	})
	return fields
}

// reviewFormFields is the full editable field order: the problem fields
// first, then the remaining fields in reading order.
func reviewFormFields(problems []reviewProblem) (first, rest []FormField) {
	first = problemFields(problems)
	seen := make(map[FormField]bool, len(first))
	for _, field := range first {
		seen[field] = true
	}
	for _, field := range reviewFieldOrder {
		if !seen[field] {
			rest = append(rest, field)
		}
	}
	return first, rest
}

// reviewProblemText joins the problems of one field for the reason line
// under it.
func reviewProblemText(problems []reviewProblem, field FormField) string {
	var messages []string
	for _, problem := range problems {
		if !problemMatchesField(problem, field) {
			continue
		}
		if !strings.Contains(strings.Join(messages, "；"), problem.Message) {
			messages = append(messages, problem.Message)
		}
	}
	return strings.Join(messages, "；")
}

// reviewConfirmBlockedReason names why "确认并下一项" is unavailable, from
// the current draft or the read-only payload.
func (m *Model) reviewConfirmBlockedReason(row *Row) string {
	if m.reviewSaving {
		return "正在保存，完成前不能确认。"
	}
	if m.loading {
		return "正在加载最新检查点，完成前不能确认。"
	}
	if row == nil {
		return ""
	}
	problems := m.reviewProblems(row)
	gaps, blocking := 0, 0
	for _, problem := range problems {
		switch problem.Kind {
		case reviewProblemBlocking:
			blocking++
		case reviewProblemGap:
			gaps++
		}
	}
	if blocking > 0 {
		return "有字段需要修正，暂时不能确认。"
	}
	if gaps > 0 {
		return fmt.Sprintf("还有 %d 个必填字段没有填好，暂时不能确认。", gaps)
	}
	if m.edit != nil && !m.edit.check.Confirmable() {
		return "必需字段仍未读取或校验未通过，不能确认。"
	}
	if row.Structured != nil && !app.CheckDiscData(row.Structured.Data).Confirmable() {
		return "必需字段仍未读取或校验未通过，不能确认。"
	}
	return ""
}

// backfillNotice is the form's top band for a failed or unprocessed item.
func backfillNotice(row *Row) string {
	if row.Backfilled {
		notice := "已补录（原：" + row.OriginalOutcomeText() + "）；原始处理结局保持不变。"
		if row.ItemClass == app.ItemClassPassed || row.ItemClass == app.ItemClassWarning {
			return notice + "当前修订可导出。"
		}
		return notice + "仍有字段待校对。"
	}
	switch row.Status {
	case record.StatusFailed:
		return "补录：这件识别失败，补全必填字段后才能导出。"
	case record.StatusUnprocessed:
		return "补录：这件没有处理，补全必填字段后才能导出。"
	}
	return ""
}

// warningNotice is the form's top band for an exportable warning item.
func warningNotice(row *Row) string {
	if row.ItemClass != app.ItemClassWarning {
		return ""
	}
	issues := row.Issues()
	for _, issue := range issues {
		if issue.Severity != zzz.SeverityWarning {
			continue
		}
		detail := issue.Message
		if issue.Field == "equipped" {
			detail = "装备者未识别，导出时留空。"
		}
		return "已纳入导出 · 警告：" + detail
	}
	return "已纳入导出 · 有警告。"
}
