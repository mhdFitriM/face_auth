package internal

import (
	"errors"
	"strings"
	"unicode/utf8"
)

// ValidateHikvisionCardNo applies the CardInfo capability advertised by the
// connected DS-K1T671MF: cardNo is a 1..20 character string. Card QR mode
// encodes this exact value, so enrollment and QR generation share one rule.
func ValidateHikvisionCardNo(raw string) (string, error) {
	cardNo := strings.TrimSpace(raw)
	length := utf8.RuneCountInString(cardNo)
	if length < 1 || length > 20 {
		return "", errors.New("cardNo must be 1 to 20 characters for this Hikvision device")
	}
	if strings.IndexFunc(cardNo, func(r rune) bool { return r < 0x20 || r == 0x7f }) >= 0 {
		return "", errors.New("cardNo must not contain control characters")
	}
	return cardNo, nil
}
