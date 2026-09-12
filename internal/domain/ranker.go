package domain

// Defines the contract for scoring algorithms used to evaluate
// the relevance of a document to a specific search term.
type Ranker interface {
	// Calculates the relevance weight of a document for a given term.
	// Parameters:
	//   - posting: The occurrence data of the term in the specific document.
	//   - docLength: The total number of tokens in the document.
	//   - avgDocLength: The average number of tokens across all documents in the index.
	//   - totalDocs: The total number of documents in the collection.
	//   - docsContainingTerm: The number of documents that contain this specific term.
	Score(posting Posting, docLength float64, avgDocLength float64, totalDocs uint64, docsContainingTerm uint64) float64
}
