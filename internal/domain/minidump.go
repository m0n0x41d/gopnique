package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"unicode/utf8"
)

const minidumpAttachmentNameMaxBytes = 512

type MinidumpAttachmentName struct {
	value string
}

type MinidumpIdentity struct {
	eventID        EventID
	attachmentName MinidumpAttachmentName
}

func NewMinidumpAttachmentName(input string) (MinidumpAttachmentName, error) {
	value := strings.TrimSpace(input)
	if value == "" {
		return MinidumpAttachmentName{}, errors.New("minidump attachment name is required")
	}

	if !utf8.ValidString(value) {
		return MinidumpAttachmentName{}, errors.New("minidump attachment name must be valid utf-8")
	}

	if len(value) > minidumpAttachmentNameMaxBytes {
		return MinidumpAttachmentName{}, errors.New("minidump attachment name is too long")
	}

	if !visibleString(value) {
		return MinidumpAttachmentName{}, errors.New("minidump attachment name must not contain control characters")
	}

	if strings.ContainsAny(value, "\\\x00") {
		return MinidumpAttachmentName{}, errors.New("minidump attachment name must not contain backslashes or null bytes")
	}

	if strings.Contains(value, "..") {
		return MinidumpAttachmentName{}, errors.New("minidump attachment name must not traverse paths")
	}

	if strings.HasPrefix(value, "/") {
		return MinidumpAttachmentName{}, errors.New("minidump attachment name must be relative")
	}

	return MinidumpAttachmentName{value: value}, nil
}

func (name MinidumpAttachmentName) String() string {
	return name.value
}

func NewMinidumpIdentity(
	eventID EventID,
	attachmentName MinidumpAttachmentName,
) (MinidumpIdentity, error) {
	if eventID.value == "" {
		return MinidumpIdentity{}, errors.New("minidump identity requires event id")
	}

	if attachmentName.value == "" {
		return MinidumpIdentity{}, errors.New("minidump identity requires attachment name")
	}

	return MinidumpIdentity{
		eventID:        eventID,
		attachmentName: attachmentName,
	}, nil
}

func (identity MinidumpIdentity) EventID() EventID {
	return identity.eventID
}

func (identity MinidumpIdentity) AttachmentName() MinidumpAttachmentName {
	return identity.attachmentName
}

func (identity MinidumpIdentity) ArtifactName() ArtifactName {
	const fieldSeparator = "\x1f"
	hasher := sha256.New()
	hasher.Write([]byte(identity.eventID.value))
	hasher.Write([]byte(fieldSeparator))
	hasher.Write([]byte(identity.attachmentName.value))
	digest := hex.EncodeToString(hasher.Sum(nil))

	return ArtifactName{value: digest + ".mdmp"}
}
