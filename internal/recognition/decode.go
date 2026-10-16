package recognition

import "strings"

// decodeCTC decodes one line of greedy CTC output (T3: 解码与分数).
//
// probs holds the [T][classes] output of a single inference, row-major.
// Per time step the highest class wins; adjacent time steps with the same raw
// index collapse into one (so a character repeated after a blank survives),
// then the blank class (index 0) is dropped. The score is the mean model
// probability of the decoded characters — a raw model score, not a field
// correctness probability. With no decoded characters the text is empty and
// the score is 0, matching upstream's conf_list=[0] fallback.
func decodeCTC(probs []float32, classes int, charset []string) (string, float64) {
	if classes <= 0 || len(charset) < classes || len(probs) == 0 {
		return "", 0
	}
	steps := len(probs) / classes
	var text strings.Builder
	var sum float64
	decoded := 0
	prev := -1
	for t := 0; t < steps; t++ {
		row := probs[t*classes : (t+1)*classes]
		best, bestProb := 0, row[0]
		for c, p := range row {
			if p > bestProb {
				best, bestProb = c, p
			}
		}
		if best == prev {
			continue
		}
		prev = best
		if best == 0 {
			continue
		}
		text.WriteString(charset[best])
		sum += float64(bestProb)
		decoded++
	}
	if decoded == 0 {
		return "", 0
	}
	return text.String(), sum / float64(decoded)
}
