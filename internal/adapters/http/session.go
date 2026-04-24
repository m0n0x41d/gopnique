package httpadapter

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"

	"github.com/ivanzakutnii/error-tracker/internal/app/operators"
)

const sessionCookieName = "error_tracker_session"

type SessionCodec struct {
	secret []byte
}

func NewSessionCodec(secret string) SessionCodec {
	return SessionCodec{secret: []byte(secret)}
}

func (codec SessionCodec) Encode(token operators.SessionToken) string {
	value := token.String()
	signature := codec.signature(value)

	return value + "." + signature
}

func (codec SessionCodec) Decode(value string) (operators.SessionToken, bool) {
	parts := strings.Split(value, ".")
	if len(parts) != 2 {
		return operators.SessionToken{}, false
	}

	if !hmac.Equal([]byte(parts[1]), []byte(codec.signature(parts[0]))) {
		return operators.SessionToken{}, false
	}

	token, tokenErr := operators.NewSessionToken(parts[0])
	if tokenErr != nil {
		return operators.SessionToken{}, false
	}

	return token, true
}

func (codec SessionCodec) signature(value string) string {
	mac := hmac.New(sha256.New, codec.secret)
	_, _ = mac.Write([]byte(value))

	return hex.EncodeToString(mac.Sum(nil))
}

func setSessionCookie(w http.ResponseWriter, codec SessionCodec, token operators.SessionToken) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    codec.Encode(token),
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   30 * 24 * 60 * 60,
	})
}

func clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}
