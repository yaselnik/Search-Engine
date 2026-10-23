package stemmer

import (
	"strings"
	"unicode/utf8"
)

type RuStemmer struct{}

func NewRuStemmer() *RuStemmer {
	return &RuStemmer{}
}

// Stem reduces a Russian word to its morphological stem using the official Snowball algorithm.
func (s *RuStemmer) Stem(word string) string {
	if utf8.RuneCountInString(word) <= 2 || IsRussianStopWord(word) {
		return word
	}

	if strings.ContainsRune(word, 'ё') {
		word = strings.ReplaceAll(word, "ё", "е")
	}

	w := NewStemWord(word)
	rv := findRussianRegions(w)
	if rv >= len(w.RS) {
		return word
	}

	// Step 1: Perfective Gerund, otherwise Reflexive + (Adjectival | Verb | Noun).
	if !ruRemovePerfectiveGerund(w, rv) {
		ruRemoveReflexive(w, rv)
		if !ruRemoveAdjectival(w, rv) {
			if !ruRemoveVerb(w, rv) {
				ruRemoveNoun(w, rv)
			}
		}
	}

	// Step 2: If the word ends with 'и' in RV, delete it.
	if len(w.RS) > rv && w.RS[len(w.RS)-1] == 'и' {
		w.RemoveLastNRunes(1)
	}

	// Step 3: Search for a Derivational suffix in R2 ('ост', 'ость') and delete it.
	ruRemoveDerivational(w)

	// Step 4: Superlative removal, undoubling 'нн', or deleting trailing soft sign 'ь'.
	ruStep4(w, rv)

	return w.ToString()
}

// findRussianRegions computes RV ( stored as return value ), R1, and R2 for a Russian word.
// RV is the region after the first vowel, or the end of the word if it contains no vowels.
func findRussianRegions(w *StemWord) int {
	n := len(w.RS)
	rv := n
	for i, r := range w.RS {
		if isRussianVowel(r) {
			rv = i + 1
			break
		}
	}

	w.R1 = firstAfterRussianVnv(w, 0)
	w.R2 = firstAfterRussianVnv(w, w.R1)
	return rv
}

func firstAfterRussianVnv(w *StemWord, start int) int {
	for i := start + 1; i < len(w.RS); i++ {
		if isRussianVowel(w.RS[i-1]) && !isRussianVowel(w.RS[i]) {
			return i + 1
		}
	}
	return len(w.RS)
}

func isRussianVowel(r rune) bool {
	switch r {
	case 'а', 'е', 'и', 'о', 'у', 'ы', 'э', 'ю', 'я':
		return true
	}
	return false
}

// hasSuffixInRegion checks if a suffix occurs strictly within the region starting at regionStart.
// If precededByAYa is true, the suffix must also be preceded by 'а' or 'я' inside the region.
func hasSuffixInRegion(w *StemWord, regionStart int, suffix string, precededByAYa bool) (int, bool) {
	if !w.HasSuffixString(suffix) {
		return 0, false
	}
	rLen := utf8.RuneCountInString(suffix)
	stemEnd := len(w.RS) - rLen
	if stemEnd < regionStart {
		return 0, false
	}
	if precededByAYa {
		if stemEnd-1 < regionStart {
			return 0, false
		}
		prev := w.RS[stemEnd-1]
		if prev != 'а' && prev != 'я' {
			return 0, false
		}
	}
	return rLen, true
}

func matchLongestSuffix(
	w *StemWord,
	regionStart int,
	group1 []string, // must be preceded by 'а' or 'я'
	group2 []string, // unconditional within region
) int {
	bestLen := 0
	for _, suf := range group1 {
		if l, ok := hasSuffixInRegion(w, regionStart, suf, true); ok && l > bestLen {
			bestLen = l
		}
	}
	for _, suf := range group2 {
		if l, ok := hasSuffixInRegion(w, regionStart, suf, false); ok && l > bestLen {
			bestLen = l
		}
	}
	return bestLen
}

var (
	perfGerundGroup1 = []string{"вшись", "вши", "в"}
	perfGerundGroup2 = []string{"ившись", "ывшись", "ивши", "ывши", "ив", "ыв"}

	reflexiveSuffixes = []string{"ся", "сь"}

	adjectiveSuffixes = []string{
		"ими", "ыми", "его", "ого", "ему", "ому", "ее", "ие", "ые", "ое",
		"ей", "ий", "ый", "ой", "ем", "им", "ым", "ом", "их", "ых",
		"ую", "юю", "ая", "яя", "ою", "ею",
	}

	participleGroup1 = []string{"ем", "нн", "вш", "ющ", "щ"}
	participleGroup2 = []string{"ивш", "ывш", "ующ"}

	verbGroup1 = []string{
		"ете", "йте", "ешь", "нно", "ла", "на", "ли", "ем", "ло",
		"но", "ет", "ют", "ны", "ть", "й", "л", "н",
	}
	verbGroup2 = []string{
		"ейте", "уйте", "ило", "ыло", "ено", "ует", "уют", "ите",
		"или", "ыли", "ила", "ыла", "ена", "ены", "ить", "ыть",
		"ишь", "ят", "ит", "ыт", "ей", "уй", "ил", "ыл", "им",
		"ым", "ен", "ую", "ю",
	}

	nounSuffixes = []string{
		"иями", "ями", "ами", "ией", "иям", "ием", "иях", "ев", "ов",
		"ие", "ье", "еи", "ии", "ей", "ой", "ий", "ям", "ем", "ам",
		"ом", "ах", "ях", "ию", "ью", "ия", "ья", "а", "е", "и",
		"й", "о", "у", "ы", "ь", "ю", "я",
	}

	derivationalSuffixes = []string{"ость", "ост"}
	superlativeSuffixes  = []string{"ейше", "ейш"}
)

func ruRemovePerfectiveGerund(w *StemWord, rv int) bool {
	if l := matchLongestSuffix(w, rv, perfGerundGroup1, perfGerundGroup2); l > 0 {
		w.RemoveLastNRunes(l)
		return true
	}
	return false
}

func ruRemoveReflexive(w *StemWord, rv int) bool {
	if l := matchLongestSuffix(w, rv, nil, reflexiveSuffixes); l > 0 {
		w.RemoveLastNRunes(l)
		return true
	}
	return false
}

func ruRemoveAdjectival(w *StemWord, rv int) bool {
	adjLen := matchLongestSuffix(w, rv, nil, adjectiveSuffixes)
	if adjLen == 0 {
		return false
	}
	w.RemoveLastNRunes(adjLen)

	// Optionally remove preceding participle suffix if present in RV.
	if partLen := matchLongestSuffix(w, rv, participleGroup1, participleGroup2); partLen > 0 {
		w.RemoveLastNRunes(partLen)
	}
	return true
}

func ruRemoveVerb(w *StemWord, rv int) bool {
	if l := matchLongestSuffix(w, rv, verbGroup1, verbGroup2); l > 0 {
		w.RemoveLastNRunes(l)
		return true
	}
	return false
}

func ruRemoveNoun(w *StemWord, rv int) bool {
	if l := matchLongestSuffix(w, rv, nil, nounSuffixes); l > 0 {
		w.RemoveLastNRunes(l)
		return true
	}
	return false
}

func ruRemoveDerivational(w *StemWord) {
	if l := matchLongestSuffix(w, w.R2, nil, derivationalSuffixes); l > 0 {
		w.RemoveLastNRunes(l)
	}
}

func ruStep4(w *StemWord, rv int) {
	if l := matchLongestSuffix(w, rv, nil, superlativeSuffixes); l > 0 {
		w.RemoveLastNRunes(l)
	}

	if _, ok := hasSuffixInRegion(w, rv, "нн", false); ok {
		w.RemoveLastNRunes(1)
		return
	}

	if _, ok := hasSuffixInRegion(w, rv, "ь", false); ok {
		w.RemoveLastNRunes(1)
	}
}

// IsRussianStopWord returns true if the input word is a common Russian stop word.
func IsRussianStopWord(word string) bool {
	switch word {
	case "и", "в", "во", "не", "что", "он", "на", "я", "с", "со", "как", "а", "то",
		"все", "она", "так", "его", "но", "да", "ты", "к", "у", "же", "вы", "за",
		"бы", "по", "только", "ее", "мне", "было", "вот", "от", "меня", "еще", "нет",
		"о", "из", "ему", "теперь", "когда", "даже", "ну", "вдруг", "ли", "если",
		"уже", "или", "ни", "быть", "был", "него", "до", "вас", "нибудь", "опять",
		"уж", "вам", "ведь", "там", "потом", "себя", "ничего", "ей", "может", "они",
		"тут", "где", "есть", "надо", "ней", "для", "мы", "тебя", "их", "чем", "была",
		"сам", "чтоб", "без", "будто", "чего", "раз", "тоже", "себе", "под", "будет",
		"ж", "тогда", "кто", "этот", "того", "потому", "этого", "какой", "совсем",
		"ним", "здесь", "этом", "один", "почти", "мой", "тем", "чтобы", "нее",
		"сейчас", "были", "куда", "зачем", "всех", "никогда", "можно", "при",
		"наконец", "два", "об", "другой", "хоть", "после", "над", "больше", "тот",
		"через", "эти", "нас", "про", "всего", "них", "какая", "много", "разве",
		"три", "эту", "моя", "впрочем", "хорошо", "свою", "этой", "перед", "иногда",
		"лучше", "чуть", "том", "нельзя", "такой", "им", "более", "всегда", "конечно",
		"всю", "между", "это":
		return true
	}
	return false
}
