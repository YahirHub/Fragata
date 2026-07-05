package onvif

import (
	"crypto/md5"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

type digestChallenge struct{ Realm, Nonce, Opaque, Algorithm, QOP string }

func parseDigestChallenge(header string) (digestChallenge, error) {
	header = strings.TrimSpace(header)
	if !strings.HasPrefix(strings.ToLower(header), "digest ") {
		return digestChallenge{}, errors.New("no es desafío digest")
	}
	header = strings.TrimSpace(header[7:])
	parts := splitAuthParts(header)
	m := map[string]string{}
	for _, p := range parts {
		k, v, ok := strings.Cut(p, "=")
		if !ok {
			continue
		}
		m[strings.ToLower(strings.TrimSpace(k))] = strings.Trim(strings.TrimSpace(v), `"`)
	}
	if m["realm"] == "" || m["nonce"] == "" {
		return digestChallenge{}, errors.New("desafío digest incompleto")
	}
	return digestChallenge{Realm: m["realm"], Nonce: m["nonce"], Opaque: m["opaque"], Algorithm: m["algorithm"], QOP: m["qop"]}, nil
}

func splitAuthParts(s string) []string {
	var out []string
	start := 0
	quoted := false
	for i, r := range s {
		if r == '"' {
			quoted = !quoted
		}
		if r == ',' && !quoted {
			out = append(out, strings.TrimSpace(s[start:i]))
			start = i + 1
		}
	}
	out = append(out, strings.TrimSpace(s[start:]))
	return out
}

func digestAuthorization(ch digestChallenge, method, uri, user, pass string) (string, error) {
	alg := strings.ToUpper(ch.Algorithm)
	if alg == "" {
		alg = "MD5"
	}
	if alg != "MD5" && alg != "SHA-256" {
		return "", fmt.Errorf("algoritmo digest no soportado: %s", alg)
	}
	h := func(v string) string {
		if alg == "SHA-256" {
			x := sha256.Sum256([]byte(v))
			return hex.EncodeToString(x[:])
		}
		x := md5.Sum([]byte(v))
		return hex.EncodeToString(x[:])
	}
	ha1 := h(user + ":" + ch.Realm + ":" + pass)
	ha2 := h(method + ":" + uri)
	response := ""
	qop := ""
	if ch.QOP != "" {
		for _, v := range strings.Split(ch.QOP, ",") {
			if strings.TrimSpace(v) == "auth" {
				qop = "auth"
				break
			}
		}
	}
	cnonceBytes := make([]byte, 12)
	if _, err := rand.Read(cnonceBytes); err != nil {
		return "", err
	}
	cnonce := hex.EncodeToString(cnonceBytes)
	nc := "00000001"
	if qop != "" {
		response = h(ha1 + ":" + ch.Nonce + ":" + nc + ":" + cnonce + ":" + qop + ":" + ha2)
	} else {
		response = h(ha1 + ":" + ch.Nonce + ":" + ha2)
	}
	parts := []string{fmt.Sprintf(`username="%s"`, escapeQuote(user)), fmt.Sprintf(`realm="%s"`, escapeQuote(ch.Realm)), fmt.Sprintf(`nonce="%s"`, escapeQuote(ch.Nonce)), fmt.Sprintf(`uri="%s"`, escapeQuote(uri)), fmt.Sprintf(`response="%s"`, response), fmt.Sprintf(`algorithm=%s`, alg)}
	if ch.Opaque != "" {
		parts = append(parts, fmt.Sprintf(`opaque="%s"`, escapeQuote(ch.Opaque)))
	}
	if qop != "" {
		parts = append(parts, "qop="+qop, "nc="+nc, fmt.Sprintf(`cnonce="%s"`, cnonce))
	}
	return "Digest " + strings.Join(parts, ", "), nil
}

func escapeQuote(s string) string { return strings.ReplaceAll(s, `"`, `\"`) }
