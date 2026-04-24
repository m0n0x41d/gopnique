package sentryprotocol

type ErrorCode string

const (
	ErrorInvalidEnvelope ErrorCode = "invalid_envelope"
	ErrorInvalidEvent    ErrorCode = "invalid_event"
)

type ProtocolError struct {
	code    ErrorCode
	message string
}

func NewProtocolError(code ErrorCode, message string) ProtocolError {
	return ProtocolError{
		code:    code,
		message: message,
	}
}

func (err ProtocolError) Error() string {
	return err.message
}

func (err ProtocolError) Code() ErrorCode {
	return err.code
}
