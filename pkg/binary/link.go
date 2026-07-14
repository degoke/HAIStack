package binary

import (
	"context"
	"fmt"
	"time"
)

// LinkService manages Binary and DocumentReference blob links.
type LinkService struct {
	meta MetadataStore
}

// NewLinkService creates a link service over a metadata store.
func NewLinkService(meta MetadataStore) *LinkService {
	return &LinkService{meta: meta}
}

// LinkBinary associates a FHIR Binary resource with a blob descriptor.
func (s *LinkService) LinkBinary(ctx context.Context, resourceID, blobID string) error {
	if resourceID == "" || blobID == "" {
		return fmt.Errorf("%w: resourceID and blobID are required", ErrInvalidArgument)
	}
	return s.meta.PutBinaryLink(ctx, BinaryLink{
		ResourceID: resourceID,
		BlobID:     blobID,
		CreatedAt:  time.Now().UTC(),
	})
}

// GetBinaryLink reads the blob link for a FHIR Binary resource.
func (s *LinkService) GetBinaryLink(ctx context.Context, resourceID string) (*BinaryLink, error) {
	return s.meta.GetBinaryLink(ctx, resourceID)
}

// DeleteBinaryLink removes a FHIR Binary blob link.
func (s *LinkService) DeleteBinaryLink(ctx context.Context, resourceID string) error {
	return s.meta.DeleteBinaryLink(ctx, resourceID)
}

// LinkDocumentAttachment associates a DocumentReference attachment with a blob.
func (s *LinkService) LinkDocumentAttachment(ctx context.Context, documentID string, contentIndex int, blobID string) error {
	if documentID == "" || blobID == "" {
		return fmt.Errorf("%w: documentID and blobID are required", ErrInvalidArgument)
	}
	if contentIndex < 0 {
		return fmt.Errorf("%w: contentIndex must be non-negative", ErrInvalidArgument)
	}
	return s.meta.PutDocumentLink(ctx, DocumentAttachmentLink{
		DocumentID:   documentID,
		ContentIndex: contentIndex,
		BlobID:       blobID,
		CreatedAt:    time.Now().UTC(),
	})
}

// GetDocumentAttachmentLink reads a DocumentReference attachment blob link.
func (s *LinkService) GetDocumentAttachmentLink(ctx context.Context, documentID string, contentIndex int) (*DocumentAttachmentLink, error) {
	return s.meta.GetDocumentLink(ctx, documentID, contentIndex)
}

// DeleteDocumentAttachmentLink removes a DocumentReference attachment blob link.
func (s *LinkService) DeleteDocumentAttachmentLink(ctx context.Context, documentID string, contentIndex int) error {
	return s.meta.DeleteDocumentLink(ctx, documentID, contentIndex)
}
