package redis

import (
	"encoding/base64"

	"github.com/degoke/haistack/pkg/oauth"
)

func issuerKeySegment(issuer string) (string, error) {
	iss, err := oauth.RequireBoundIssuer(issuer)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString([]byte(iss)), nil
}

func authCodeRedisKey(prefix, issuer, code string) (string, error) {
	seg, err := issuerKeySegment(issuer)
	if err != nil {
		return "", err
	}
	return prefix + "authcode:" + seg + ":" + code, nil
}

func pendingRedisKey(prefix, issuer, id string) (string, error) {
	seg, err := issuerKeySegment(issuer)
	if err != nil {
		return "", err
	}
	return prefix + "pending:" + seg + ":" + id, nil
}

func refreshRedisKey(prefix, issuer, token string) (string, error) {
	seg, err := issuerKeySegment(issuer)
	if err != nil {
		return "", err
	}
	return prefix + "refresh:" + seg + ":" + token, nil
}
