package server

import (
	"errors"
	"testing"
)

func TestParseV2ResponseRejectsInvalidJSON(t *testing.T) {
	_, err := parseV2Response([]byte("<!DOCTYPE html><html></html>"))
	if err == nil {
		t.Fatal("expected invalid v2 response error")
	}
}

func TestParseV2ResponseRejectsWrongVersion(t *testing.T) {
	_, err := parseV2Response([]byte(`{"jsonrpc":"1.0","id":"x","result":{}}`))
	if err == nil {
		t.Fatal("expected invalid v2 version error")
	}
}

func TestNetworkErrorsAreNotJSONParseOrHTTPStatus(t *testing.T) {
	err := errors.New("dial tcp: lookup example.com: no such host")
	if isHTTPStatus(err, 404) {
		t.Fatal("network errors must not be treated as HTTP status errors")
	}
	_, parseErr := parseV2Response([]byte("not json"))
	if parseErr == nil {
		t.Fatal("invalid JSON must fail parseV2Response")
	}
}

func TestHTTPStatusClassification(t *testing.T) {
	err := &httpStatusError{StatusCode: 404, Status: "404 Not Found"}
	if !isHTTPStatus(err, 404) {
		t.Fatal("expected 404 HTTP status")
	}
	if isHTTPStatus(err, 500) {
		t.Fatal("404 should not match 500")
	}
}
