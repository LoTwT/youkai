package recognition

import "strings"

// blankToken occupies index 0 of the decode table. Upstream inserts the
// literal string "blank" at position 0; the CTC decode drops index 0 before
// mapping indices to characters, so the placeholder text is never emitted.
const blankToken = "blank"

// buildCharset assembles the decode table from the model's "character"
// metadata (T3: 字符顺序): one entry per line in metadata order, never
// reordered, with the CTC blank inserted at index 0 and a single space
// appended at the end. The caller validates that the resulting length equals
// the model's output class count; for the pinned model this is
// 1 + 18,708 + 1 = 18,710.
func buildCharset(meta string) []string {
	lines := splitMetadataLines(meta)
	table := make([]string, 0, len(lines)+2)
	table = append(table, blankToken)
	table = append(table, lines...)
	table = append(table, " ")
	return table
}

// splitMetadataLines splits the metadata string the way Python's
// str.splitlines does for the separators that occur in ONNX character
// metadata: \n and \r\n. Interior empty lines are preserved, like upstream
// (RapidOCR's get_character_list uses meta[key].splitlines() directly); the
// pinned model's metadata contains 18,708 single-character lines with no \r
// and no trailing newline, verified against its SHA-256 in T3.
func splitMetadataLines(s string) []string {
	if s == "" {
		return nil
	}
	lines := strings.Split(s, "\n")
	// A trailing newline ends the last line and does not open an empty one
	// ("a\n".splitlines() == ["a"]).
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	for i, l := range lines {
		lines[i] = strings.TrimSuffix(l, "\r")
	}
	return lines
}
