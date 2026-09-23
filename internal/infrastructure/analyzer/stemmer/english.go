// Package stemmer implements the English Porter2 (Snowball) stemming algorithm.
//
// # References and Credits
//
// Snowball Implementation: The Snowball stemming language and reference implementations.
// See: https://snowballstem.org/algorithms/english/stemmer.html
//
// This implementation is heavily inspired by and adapted from:
// https://github.com/kljensen/snowball
package stemmer

import "unicode/utf8"

var exceptionalPrefixes []string = []string{
	"gener",   // generate/general/generic/generous
	"commun",  // communication/communism/community
	"arsen",   // arsenic/arsenal
	"past",    // past/paste
	"univers", // universe/universal/university
	"later",   // lateral/later
	"emerg",   // emerge/emergency
	"organ",   // organ/organic/organize
	"inter",   // intern/internal/international/internment; interfere; interval
}

type EngStemmer struct {
}

func NewEngStemmer() *EngStemmer{
	return &EngStemmer{}
}

// Stem reduces an English word to its root form using the Porter2 algorithm.
// Note: Apostrophe handling is not supported.
func (s *EngStemmer) Stem(word string) string {

	if len(word) <= 2 || IsStopWord(word) {
		return word
	}

	if isSpecial := stemSpecialWord(word); isSpecial != "" {
		return isSpecial
	}

	w := NewStemWord(word)
	capitalizeY(w)
	setR1R2(w)
	step1a(w)
	step1b(w)
	step1c(w)
	step2(w)
	step3(w)
	step4(w)
	step5(w)
	uncapitalizeY(w)

	return w.ToString()
}

// Step 1a:
//
// Search for the longest among the following suffixes, and perform the action indicated.
//
// sses:     replace by ss;
// ied, ies: replace by i if preceded by more than one letter, otherwise by ie;
// s:        delete if the preceding word part contains a vowel not immediately before the s;
// us, ss:   do nothing;
func step1a(w *StemWord) {

	suffix := w.FirstSuffix("sses", "ied", "ies", "us", "ss", "s")

	switch suffix {

	case "sses":
		w.ReplaceSuffix([]rune(suffix), []rune("ss"))
		return
	case "ies", "ied":
		var repl string
		if len(w.RS) > 4 {
			repl = "i"
		} else {
			repl = "ie"
		}
		w.ReplaceSuffix([]rune(suffix), []rune(repl))
		return
	case "us", "ss":
		return
	case "s":
		suffixLength := utf8.RuneCountInString(suffix)
		for i := 0; i < len(w.RS)-2; i++ {
			if isLowerVowel(w.RS[i]) {
				w.RemoveLastNRunes(suffixLength)
				return
			}
		}
	default:
		return
	}
}

// Step 1b:
//
// Search for the longest among the following suffixes, and perform the action indicated.
//
// eed eedly: replace by ee if in R1
//
// ed edly ing ingly:
// for ing, check if the word before the suffix is exactly one of the following exceptional cases:
// if it's a non-vowel followed by y, replace y and ing with ie (so dying → die), then go to step 1c.
// if it's exactly one of inn, out, cann, herr, earr or even then go to step 1c.
// delete if the preceding word part contains a vowel, and after the deletion:
// if the word ends at, bl or iz add e (so luxuriat → luxuriate), or
// if the word ends with a double preceded by something other than exactly a, e or o then remove the last letter (so hopp → hop but add, egg and off are not changed), or
// if the word does not end with a double and is short, add e (so hop → hope)
func step1b(w *StemWord) {

	suffix := w.FirstSuffix("eedly", "ingly", "edly", "ing", "eed", "ed")
	suffixLength := utf8.RuneCountInString(suffix)

	switch suffix {
	case "":
		return

	case "eed", "eedly":
		if suffixLength <= len(w.RS)-w.R1 {
			w.ReplaceSuffix([]rune(suffix), []rune("ee"))
		}
		return

	case "ed", "edly", "ing", "ingly":
		hasVowel := false
		for i := 0; i < len(w.RS)-suffixLength; i++ {
			if isLowerVowel(w.RS[i]) {
				hasVowel = true
				break
			}
		}
		if hasVowel {
			originalR1 := w.R1
			originalR2 := w.R2

			if suffix == "ing" {
				stemRunes := w.RS[:len(w.RS)-suffixLength]
				stemStr := string(stemRunes)

				if len(stemRunes) == 2 {
					if stemRunes[len(stemRunes)-1] == 'y' && !isLowerVowel(stemRunes[len(stemRunes)-2]) {
						w.RS = append(stemRunes[:len(stemRunes)-1], []rune("ie")...)
						restoreR1R2(w, originalR1, originalR2)
						return
					}
				}

				if stemStr == "even" || stemStr == "inn" || stemStr == "out" ||
					stemStr == "cann" || stemStr == "herr" || stemStr == "earr" {
					return
				}
			}

			w.RemoveLastNRunes(suffixLength)

			newSuffix := w.FirstSuffix("at", "bl", "iz", "bb", "dd", "ff", "gg", "mm", "nn", "pp", "rr", "tt")
			switch newSuffix {

			case "":
				if isShortWord(w) {
					w.RS = append(w.RS, []rune("e")...)
					w.R1 = len(w.RS)
					w.R2 = len(w.RS)
					return
				}

			case "at", "bl", "iz":
				w.ReplaceSuffix([]rune(newSuffix), []rune(newSuffix+"e"))
			case "bb", "dd", "ff", "gg", "mm", "nn", "pp", "rr", "tt":
				if len(w.RS) == 3 {
					if w.RS[len(w.RS)-3] != 'a' && w.RS[len(w.RS)-3] != 'e' && w.RS[len(w.RS)-3] != 'o' {
						w.RemoveLastNRunes(1)
					}
				} else {
					w.RemoveLastNRunes(1)
				}
			}

			restoreR1R2(w, originalR1, originalR2)
			return
		}
	default:
		return
	}
}

// Step 1c:
//
// Replaces suffix y or Y by i if preceded by a non-vowel which is not the first letter of the word
// (a consequence of this condition is that this step only affects strings of length 3 or more).
func step1c(w *StemWord) {
	wLen := len(w.RS)

	if len(w.RS) > 2 && (w.RS[wLen-1] == 'y' || w.RS[wLen-1] == 'Y') && !isLowerVowel(w.RS[wLen-2]) {
		w.RS[wLen-1] = 'i'
	}
}

// Step 2:
//
// Search for the longest among the following suffixes, and, if found and in R1, perform the action indicated.
//
// tional:               replace by tion;
// enci:                 replace by ence;
// anci:                 replace by ance;
// abli:                 replace by able;
// entli:                replace by ent;
// izer, ization:        replace by ize;
// ational, ation, ator: replace by ate;
// alism, aliti, alli:   replace by al;
// fulness:              replace by ful;
// ousli, ousness:       replace by ous;
// iveness, iviti:       replace by ive;
// biliti, bli:          replace by ble;
// ogist:                replace by og;
// ogi:                  replace by og if preceded by l;
// fulli:                replace by ful;
// lessli:               replace by less;
// li:                   delete if preceded by a valid li-ending.
func step2(w *StemWord) {

	suffix := w.FirstSuffix(
		"ational", "fulness", "iveness", "ization", "ousness",
		"biliti", "lessli", "tional", "alism", "aliti", "ation",
		"entli", "fulli", "iviti", "ousli", "ogist", "anci", "abli",
		"alli", "ator", "enci", "izer", "bli", "ogi", "li",
	)
	suffixLength := utf8.RuneCountInString(suffix)

	if suffix == "" || suffixLength > len(w.RS)-w.R1 {
		return
	}

	switch suffix {
	case "li":
		wLen := len(w.RS)
		if wLen >= 3 {
			switch w.RS[wLen-3] {
			case 'c', 'd', 'e', 'g', 'h', 'k', 'm', 'n', 'r', 't':
				w.RemoveLastNRunes(suffixLength)
			}
		}
		return
	case "ogi":
		wLen := len(w.RS)
		if wLen >= 4 && w.RS[wLen-4] == 'l' {
			w.ReplaceSuffix([]rune(suffix), []rune("og"))
		}
		return
	}

	var repl string
	switch suffix {
	case "tional":
		repl = "tion"
	case "enci":
		repl = "ence"
	case "anci":
		repl = "ance"
	case "abli":
		repl = "able"
	case "entli":
		repl = "ent"
	case "izer", "ization":
		repl = "ize"
	case "ational", "ation", "ator":
		repl = "ate"
	case "alism", "aliti", "alli":
		repl = "al"
	case "fulness":
		repl = "ful"
	case "ousli", "ousness":
		repl = "ous"
	case "iveness", "iviti":
		repl = "ive"
	case "biliti", "bli":
		repl = "ble"
	case "fulli":
		repl = "ful"
	case "lessli":
		repl = "less"
	case "ogist":
		repl = "og"
	}
	w.ReplaceSuffix([]rune(suffix), []rune(repl))
}

// Step 3:
//
// Search for the longest among the following suffixes, and,
// if found and in R1, perform the action indicated.
//
// tional:           replace by tion;
// ational:          replace by ate;
// alize:            replace by al;
// icate iciti ical: replace by ic;
// ful ness:         delete;
// ative:            delete if in R2.
func step3(w *StemWord) {

	suffix := w.FirstSuffix(
		"ational", "tional", "alize", "icate", "ative",
		"iciti", "ical", "ful", "ness",
	)

	suffixLength := utf8.RuneCountInString(suffix)

	if suffix == "" || suffixLength > len(w.RS)-w.R1 {
		return
	}

	if suffix == "ative" {
		if len(w.RS)-w.R2 >= 5 {
			w.RemoveLastNRunes(suffixLength)
		}
		return
	}

	var repl string
	switch suffix {
	case "ational":
		repl = "ate"
	case "tional":
		repl = "tion"
	case "alize":
		repl = "al"
	case "icate", "iciti", "ical":
		repl = "ic"
	case "ful", "ness":
		repl = ""
	}
	w.ReplaceSuffix([]rune(suffix), []rune(repl))
}

// Step 4:
//
// Search for the longest among the following suffixes,
// and, if found and in R2, perform the action indicated.
//
// al, ance, ence, er, ic, able, ible, ant, ement, ment,
// ent, ism, ate, iti, ous, ive, ize: delete;
// ion: delete if preceded by s or t.
func step4(w *StemWord) {

	suffix := w.FirstSuffix(
		"ement", "ance", "ence", "able", "ible", "ment",
		"ent", "ant", "ism", "ate", "iti", "ous", "ive",
		"ize", "ion", "al", "er", "ic",
	)
	suffixLength := utf8.RuneCountInString(suffix)

	if suffixLength > len(w.RS)-w.R2 {
		return
	}

	switch suffix {
	case "":
		return

	case "ion":
		wLen := len(w.RS)
		if wLen >= 4 {
			switch w.RS[wLen-4] {
			case 's', 't':
				w.RemoveLastNRunes(suffixLength)
			}
		}
		return
	}

	w.RemoveLastNRunes(suffixLength)
}

// Step 5:
//
// Search for the following suffixes, and, if found, perform the action indicated.
//
// e: delete if in R2, or in R1 and not preceded by a short syllable;
// l: delete if in R2 and preceded by l.
func step5(w *StemWord) {

	// Last rune index = `lri`
	lri := len(w.RS) - 1

	if w.R1 > lri {
		return
	}

	if w.RS[lri] == 'e' {
		if w.R2 <= lri || !endsShortSyllable(w, lri) {
			w.ReplaceSuffix([]rune("e"), []rune(""))
		}
	} else if w.R2 <= lri && w.RS[lri] == 'l' && lri-1 >= 0 && w.RS[lri-1] == 'l' {
		w.ReplaceSuffix([]rune("l"), []rune(""))
	}
}

// Calculates the boundaries of regions R1 and R2 for the word.
func setR1R2(w *StemWord) {

	specialPrefix := w.FirstPrefix(exceptionalPrefixes...)

	if specialPrefix != "" {
		w.R1 = len(specialPrefix)
	} else {
		w.R1 = firstAfterVnv(w, 0)
	}
	w.R2 = firstAfterVnv(w, w.R1)
}

// Capitalize all 'Y's preceded by vowels or starting a word
func capitalizeY(w *StemWord) {
	for i, r := range w.RS {
		if r == 'y' && (i == 0 || isLowerVowel(w.RS[i-1])) {
			w.RS[i] = 'Y'
		}
	}
}

// Uncapitalize all 'Y's
func uncapitalizeY(w *StemWord) {
	for i, r := range w.RS {
		if r == 'Y' {
			w.RS[i] = 'y'
		}
	}
}

// Checks if a rune is a lowercase English vowel.
func isLowerVowel(r rune) bool {
	switch r {
	case 'a', 'e', 'i', 'o', 'u', 'y':
		return true
	}
	return false
}

// Finds the region after the first non-vowel following a vowel,
// or a the null region at the end of the word if there is no
// such non-vowel. Returns the index in the word where the
// the region starts. Pos - starting position for a search.
func firstAfterVnv(w *StemWord, pos int) int {
	for i := pos + 1; i < len(w.RS); i++ {
		if isLowerVowel(w.RS[i-1]) && !isLowerVowel(w.RS[i]) {
			return i + 1
		}
	}
	return len(w.RS)
}

// Returns the stemmed version of a word if it is a special
// case, otherwise returns the empty string.
func stemSpecialWord(word string) string {
	switch word {
	case "skis":
		return "ski"
	case "skies":
		return "sky"
	case "dying":
		return "die"
	case "lying":
		return "lie"
	case "tying":
		return "tie"
	case "idly":
		return "idl"
	case "gently":
		return "gentl"
	case "ugly":
		return "ugli"
	case "early":
		return "earli"
	case "only":
		return "onli"
	case "singly":
		return "singl"
	case "sky":
		return "sky"
	case "news":
		return "news"
	case "howe":
		return "howe"
	case "atlas":
		return "atlas"
	case "cosmos":
		return "cosmos"
	case "bias":
		return "bias"
	case "andes":
		return "andes"
	case "inning":
		return "inning"
	case "innings":
		return "inning"
	case "outing":
		return "outing"
	case "outings":
		return "outing"
	case "canning":
		return "canning"
	case "cannings":
		return "canning"
	case "herring":
		return "herring"
	case "herrings":
		return "herring"
	case "earring":
		return "earring"
	case "earrings":
		return "earring"
	case "paste":
		return "paste"
	case "proceed":
		return "proceed"
	case "proceeds":
		return "proceed"
	case "proceeded":
		return "proceed"
	case "proceeding":
		return "proceed"
	case "exceed":
		return "exceed"
	case "exceeds":
		return "exceed"
	case "exceeded":
		return "exceed"
	case "exceeding":
		return "exceed"
	case "succeed":
		return "succeed"
	case "succeeds":
		return "succeed"
	case "succeeded":
		return "succeed"
	case "succeeding":
		return "succeed"
	default:
		return ""
	}
}

// Return `true` if the input `word` is an English stop word.
func IsStopWord(word string) bool {
	switch word {
	case "a", "about", "above", "after", "again", "against", "all", "am", "an",
		"and", "any", "are", "as", "at", "be", "because", "been", "before",
		"being", "below", "between", "both", "but", "by", "can", "did", "do",
		"does", "doing", "don", "down", "during", "each", "few", "for", "from",
		"further", "had", "has", "have", "having", "he", "her", "here", "hers",
		"herself", "him", "himself", "his", "how", "i", "if", "in", "into", "is",
		"it", "its", "itself", "just", "me", "more", "most", "my", "myself",
		"no", "nor", "not", "now", "of", "off", "on", "once", "only", "or",
		"other", "our", "ours", "ourselves", "out", "over", "own", "s", "same",
		"she", "should", "so", "some", "such", "t", "than", "that", "the", "their",
		"theirs", "them", "themselves", "then", "there", "these", "they",
		"this", "those", "through", "to", "too", "under", "until", "up",
		"very", "was", "we", "were", "what", "when", "where", "which", "while",
		"who", "whom", "why", "will", "with", "you", "your", "yours", "yourself",
		"yourselves":
		return true
	}
	return false
}

// A word is called short if it ends in a short syllable, and if R1 is null.
func isShortWord(w *StemWord) bool {

	wLen := len(w.RS)
	if w.R1 < wLen {
		return false
	}

	return endsShortSyllable(w, wLen)
}

// Define a short syllable in a word as either (a) a vowel followed by a non-vowel other than w, x or Y and preceded by a non-vowel,
// (b) a vowel at the beginning of the word followed by a non-vowel, or (c) past.
func endsShortSyllable(w *StemWord, i int) bool {

	if w.ToString() == "past" {
		return true
	}

	if i == 2 {
		if isLowerVowel(w.RS[0]) && !isLowerVowel(w.RS[1]) {
			return true
		} else {
			return false
		}

	} else if i >= 3 {
		s1 := w.RS[i-1]
		s2 := w.RS[i-2]
		s3 := w.RS[i-3]

		if !isLowerVowel(s1) && s1 != 'w' && s1 != 'x' && s1 != 'Y' && isLowerVowel(s2) && !isLowerVowel(s3) {
			return true
		} else {
			return false
		}
	}

	return false
}

// Safely restores R1 and R2 boundaries after word modification.
func restoreR1R2(w *StemWord, oldR1, oldR2 int) {
	wLen := len(w.RS)

	if oldR1 < wLen {
		w.R1 = oldR1
	} else {
		w.R1 = wLen
	}
	if oldR2 < wLen {
		w.R2 = oldR2
	} else {
		w.R2 = wLen
	}
}
