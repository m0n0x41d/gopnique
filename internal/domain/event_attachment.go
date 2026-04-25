package domain

import (
	"errors"
	"strings"
	"unicode/utf8"
)

const eventAttachmentContentTypeMaxBytes = 255

type EventAttachment struct {
	kind        ArtifactKind
	name        ArtifactName
	byteSize    int64
	contentType string
}

func NewEventAttachment(
	kind ArtifactKind,
	name ArtifactName,
	byteSize int64,
	contentType string,
) (EventAttachment, error) {
	if kind.value == "" {
		return EventAttachment{}, errors.New("event attachment requires kind")
	}

	if name.value == "" {
		return EventAttachment{}, errors.New("event attachment requires name")
	}

	if byteSize < 0 {
		return EventAttachment{}, errors.New("event attachment byte size must not be negative")
	}

	normalizedContentType, contentTypeErr := normalizeAttachmentContentType(contentType)
	if contentTypeErr != nil {
		return EventAttachment{}, contentTypeErr
	}

	return EventAttachment{
		kind:        kind,
		name:        name,
		byteSize:    byteSize,
		contentType: normalizedContentType,
	}, nil
}

func (attachment EventAttachment) Kind() ArtifactKind {
	return attachment.kind
}

func (attachment EventAttachment) Name() ArtifactName {
	return attachment.name
}

func (attachment EventAttachment) ByteSize() int64 {
	return attachment.byteSize
}

func (attachment EventAttachment) ContentType() string {
	return attachment.contentType
}

func normalizeAttachmentContentType(input string) (string, error) {
	value := strings.TrimSpace(input)
	if value == "" {
		return "", nil
	}

	if !utf8.ValidString(value) {
		return "", errors.New("event attachment content type must be valid utf-8")
	}

	if len(value) > eventAttachmentContentTypeMaxBytes {
		return "", errors.New("event attachment content type is too long")
	}

	for _, runeValue := range value {
		if runeValue < 0x20 || runeValue == 0x7f {
			return "", errors.New("event attachment content type must not contain control characters")
		}
	}

	return value, nil
}

func copyEventAttachments(attachments []EventAttachment) []EventAttachment {
	if len(attachments) == 0 {
		return nil
	}

	copied := make([]EventAttachment, len(attachments))
	copy(copied, attachments)

	return copied
}
