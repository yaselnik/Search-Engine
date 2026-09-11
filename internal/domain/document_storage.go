package domain

import "context"

type DocumentStorage interface {
	Add(ctx context.Context, doc Document) error

	GetDocumentByID(ctx context.Context, id DocID) (Document, error)

	GetAll(ctx context.Context) ([]Document, error)

	Delete(ctx context.Context, id DocID) error

	Exists(ctx context.Context, id DocID) (bool, error)
}
