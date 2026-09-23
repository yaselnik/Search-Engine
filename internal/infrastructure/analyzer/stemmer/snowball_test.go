package stemmer

import (
	"bufio"
	"os"
	"strings"
	"testing"
)

func TestEngStemmerWithSnowballOfficial(t *testing.T) {
	vocFile, err := os.Open("testdata/eng_voc.txt")
	if err != nil {
		t.Fatalf("Failed to open testdata/eng_voc.txt: %v", err)
	}
	defer vocFile.Close()

	outFile, err := os.Open("testdata/eng_output.txt")
	if err != nil {
		t.Fatalf("Failed to open testdata/eng_output.txt: %v", err)
	}
	defer outFile.Close()

	vocScanner := bufio.NewScanner(vocFile)
	outScanner := bufio.NewScanner(outFile)

	var total, passed, failed int

	EngStemmer := NewEngStemmer()
	for vocScanner.Scan() && outScanner.Scan() {
		input := strings.TrimSpace(vocScanner.Text())
		expected := strings.TrimSpace(outScanner.Text())

		if input == "" {
			continue
		}

		total++

		result := EngStemmer.Stem(input)
		if result != expected {
			failed++
		} else {
			passed++
		}
	}

	if err := vocScanner.Err(); err != nil {
		t.Fatalf("Reading error voc.txt: %v", err)
	}
	if err := outScanner.Err(); err != nil {
		t.Fatalf("Reading error output.txt: %v", err)
	}

	t.Logf("Snowball Test Suite: Total=%d, Passed=%d, Failed=%d (%.2f%%)",
		total, passed, failed, float64(passed)/float64(total)*100)
}
