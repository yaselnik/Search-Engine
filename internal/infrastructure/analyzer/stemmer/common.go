package stemmer

import "unicode/utf8"

// StemWord represents a word being processed by the stemmer,
// including its rune slice and the boundaries of regions R1 and R2.
type StemWord struct {
	RS []rune

	// R1 is the region after the first non-vowel following a vowel,
	// or is the null region at the end of the word if there is no such non-vowel.
	R1 int

	// R2 is the region after the first non-vowel following a vowel in R1,
	// or is the null region at the end of the word if there is no such non-vowel.
	R2 int
}

func NewStemWord(word string) *StemWord {
	w := &StemWord{RS: []rune(word)}
	w.R1 = len(w.RS)
	w.R2 = len(w.RS)
	return w
}

func (w *StemWord) ToString() string {
	return string(w.RS)
}

// FirstPrefix returns the first matching prefix found or the empty string.
func (w *StemWord) FirstPrefix(prefixes ...string) string {
	rsLen := len(w.RS)

	for _, prefix := range prefixes {
		if utf8.RuneCountInString(prefix) > rsLen {
			continue
		}

		matched := true
		idx := 0
		for _, r := range prefix {
			if w.RS[idx] != r {
				matched = false
				break
			}
			idx++
		}
		if matched {
			return prefix
		}
	}
	return ""
}

// FirstSuffix returns the first matching suffix from the provided candidates without allocating rune slices.
func (w *StemWord) FirstSuffix(suffixes ...string) string {
	for _, suffix := range suffixes {
		if w.HasSuffixString(suffix) {
			return suffix
		}
	}
	return ""
}

// HasSuffixString checks whether w ends with the given UTF-8 suffix string with zero heap allocations.
func (w *StemWord) HasSuffixString(suffix string) bool {
	i := len(w.RS)
	for len(suffix) > 0 {
		if i == 0 {
			return false
		}
		r, size := utf8.DecodeLastRuneInString(suffix)
		suffix = suffix[:len(suffix)-size]
		i--
		if w.RS[i] != r {
			return false
		}
	}
	return true
}

func (w *StemWord) ReplaceSuffix(old []rune, new []rune) {
	lenWithoutSuffix := len(w.RS) - len(old)
	w.RS = append(w.RS[:lenWithoutSuffix], new...)
	w.resetR1R2()
}

// resetR1R2 ensures R1 and R2 remain within the bounds of the current rune slice.
func (w *StemWord) resetR1R2() {
	rsLen := len(w.RS)
	if w.R1 > rsLen {
		w.R1 = rsLen
	}
	if w.R2 > rsLen {
		w.R2 = rsLen
	}
}

func (w *StemWord) RemoveLastNRunes(n int) {
	w.RS = w.RS[:len(w.RS)-n]
	w.resetR1R2()
}
