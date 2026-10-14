// Copyright (c) Privasys. All rights reserved.
// Licensed under the GNU Affero General Public License v3.0. See LICENSE.

package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// sign returns the hex HMAC-SHA256 of body under secret, which is what the
// sender puts in X-Demo-Signature.
func sign(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// verifySignature checks a sender's signature in constant time. A missing
// or malformed signature is simply a bad one.
func verifySignature(secret string, body []byte, signature string) bool {
	got, err := hex.DecodeString(strings.TrimSpace(signature))
	if err != nil || len(got) == 0 {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hmac.Equal(got, mac.Sum(nil))
}
