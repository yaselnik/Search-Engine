package stemmer

import (
	"bufio"
	"os"
	"strings"
	"testing"
)

func runSnowballSuite(t *testing.T, name string, st Stemmer, vocPath, outPath string) {
	t.Helper()

	vocFile, err := os.Open(vocPath)
	if err != nil {
		t.Fatalf("Failed to open %s: %v", vocPath, err)
	}
	defer vocFile.Close()

	outFile, err := os.Open(outPath)
	if err != nil {
		t.Fatalf("Failed to open %s: %v", outPath, err)
	}
	defer outFile.Close()

	vocScanner := bufio.NewScanner(vocFile)
	outScanner := bufio.NewScanner(outFile)

	var total, passed, failed int

	for vocScanner.Scan() && outScanner.Scan() {
		input := strings.TrimSpace(vocScanner.Text())
		expected := strings.TrimSpace(outScanner.Text())

		if input == "" {
			continue
		}

		total++

		result := st.Stem(input)
		if result != expected {
			failed++
			if failed <= 10 {
				t.Logf("[%s mismatch] input=%q: got=%q, expected=%q", name, input, result, expected)
			}
		} else {
			passed++
		}
	}

	if err := vocScanner.Err(); err != nil {
		t.Fatalf("Reading error %s: %v", vocPath, err)
	}
	if err := outScanner.Err(); err != nil {
		t.Fatalf("Reading error %s: %v", outPath, err)
	}

	passRate := float64(passed) / float64(total) * 100
	t.Logf("%s Snowball Test Suite: Total=%d, Passed=%d, Failed=%d (%.2f%%)",
		name, total, passed, failed, passRate)

	if passRate < 99.0 {
		t.Errorf("%s stemmer accuracy is below 99%%: got %.2f%%", name, passRate)
	}
}

func TestEngStemmerWithSnowballOfficial(t *testing.T) {
	runSnowballSuite(t, "English", NewEngStemmer(), "testdata/eng_voc.txt", "testdata/eng_output.txt")
}

func TestRuStemmerWithSnowballOfficial(t *testing.T) {
	runSnowballSuite(t, "Russian", NewRuStemmer(), "testdata/ru_voc.txt", "testdata/ru_output.txt")
}
