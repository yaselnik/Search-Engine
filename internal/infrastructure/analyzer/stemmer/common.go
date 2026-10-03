package stemmer

// StemWord represents a word being processed by the stemmer,
// including its rune slice and the boundaries of regions R1 and R2.
type StemWord struct {
	RS []rune

	//R1 is the region after the first non-vowel following a vowel,
	//or is the null region at the end of the word if there is no such non-vowel.
	R1 int

	//R2 is the region after the first non-vowel following a vowel in R1,
	//or is the null region at the end of the word if there is no such non-vowel.
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

// Return the first prefix found or the empty string.
func (w *StemWord) FirstPrefix(prefixes ...string) string {
	found := false
	rsLen := len(w.RS)

	for _, prefix := range prefixes {
		prefixRunes := []rune(prefix)
		if len(prefixRunes) > rsLen {
			continue
		}

		found = true
		for i, r := range prefixRunes {
			if (w.RS)[i] != r {
				found = false
				break
			}
		}
		if found {
			return prefix
		}
	}
	return ""
}

func (w *StemWord) FirstSuffix(suffixes ...string) string {
	for _, suffix := range suffixes {
		suffixRunes := []rune(suffix)
		if w.HasSuffix(suffixRunes) {
			return suffix
		}
	}

	return ""
}

func (w *StemWord) HasSuffix(suffixRunes []rune) bool {
	wLen := len(w.RS)
	suffixLen := len(suffixRunes)
	if suffixLen > wLen {
		return false
	}

	for i := 0; i < suffixLen; i++ {
		if w.RS[wLen-i-1] != suffixRunes[suffixLen-i-1] {
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

// Resets R1 and R2 to ensure they
// are within bounds of the current rune slice.
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
