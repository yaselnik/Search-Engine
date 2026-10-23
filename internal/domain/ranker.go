package domain

// Defines the contract for scoring algorithms used to evaluate
// the relevance of a document to a specific search term.
type Ranker interface {
	// Calculates the relevance weight of a document for a given term.
	// Parameters:
	//   - posting: The occurrence data of the term in the specific document.
	//   - docStats: The total number of tokens in the document's title and content.
	//   - corpusStats: Contains the total number of documents in the index and the average number of tokens across all documents' titles and contents in the index.
	//   - docsContainingTerm: The number of documents that contain this specific term.
	Score(posting Posting, docStats DocStats, corpusStats CorpusStats, docsContainingTerm uint64) float64
}
