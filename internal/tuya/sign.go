package tuya

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
)

// Tuya Cloud signs every request with HMAC-SHA256 over a canonical string:
//
//	contentSHA256 = sha256Hex(body)            // sha256("") for empty bodies
//	stringToSign  = METHOD + "\n" + contentSHA256 + "\n" + "\n" + path?query
//
// A token request signs client_id + t + stringToSign, a business request signs
// client_id + access_token + t + stringToSign, where t is the Unix timestamp in
// milliseconds. The digest is hex-encoded and uppercase.

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func hmacSHA256Hex(key, message string) string {
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(message))
	return strings.ToUpper(hex.EncodeToString(mac.Sum(nil)))
}

func canonicalString(method, contentSHA256, pathWithQuery string) string {
	return method + "\n" + contentSHA256 + "\n\n" + pathWithQuery
}

func signToken(clientID, secret string, t int64, method, contentSHA256, pathWithQuery string) string {
	return hmacSHA256Hex(secret, clientID+strconv.FormatInt(t, 10)+canonicalString(method, contentSHA256, pathWithQuery))
}

func signBusiness(clientID, accessToken, secret string, t int64, method, contentSHA256, pathWithQuery string) string {
	return hmacSHA256Hex(secret, clientID+accessToken+strconv.FormatInt(t, 10)+canonicalString(method, contentSHA256, pathWithQuery))
}
