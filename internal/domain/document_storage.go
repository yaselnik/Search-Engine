package domain

import "context"

// DocumentStorage defines the persistence contract for parsed web documents.
type DocumentStorage interface {
	// Add inserts a new document or updates an existing one, marking it as unindexed.
	Add(ctx context.Context, doc Document) error

	// Count returns the total number of documents currently stored.
	Count(ctx context.Context) (int64, error)

	// GetDocumentByIDs retrieves a batch documents by their IDs.
	GetDocumentsByIDs(ctx context.Context, ids []DocID) (map[DocID]Document, error)

	// GetAll returns all stored documents.
	GetAll(ctx context.Context) ([]Document, error)

	// GetUnindexed retrieves up to limit documents that have not yet been indexed
	// or have been updated since their last indexing pass.
	GetUnindexed(ctx context.Context, limit int) ([]Document, error)

	// MarkIndexed marks the specified document IDs as indexed.
	MarkIndexed(ctx context.Context, ids []DocID) error

	// Delete removes a document by its ID.
	Delete(ctx context.Context, id DocID) error

	// Exists checks whether a document with the given ID is present in storage.
	Exists(ctx context.Context, id DocID) (bool, error)
}
