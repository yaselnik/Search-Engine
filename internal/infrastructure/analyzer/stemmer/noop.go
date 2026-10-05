package stemmer

type NoopStemmer struct{}

func (NoopStemmer) Stem(word string) string { return word }
