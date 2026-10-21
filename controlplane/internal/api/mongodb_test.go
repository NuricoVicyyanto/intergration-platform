package api

import (
	"net/url"
	"testing"
)

func TestMongoConnectionString(t *testing.T) {
	endpoint := map[string]any{
		"auth_source": "admin",
		"sslmode":     "require",
	}
	raw := mongoConnectionString(endpoint, "mongo.internal", "27017", "app", "user", "p@ss word", "require")
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	if parsed.Scheme != "mongodb" || parsed.Host != "mongo.internal:27017" || parsed.Path != "/app" {
		t.Fatalf("Mongo URI = %s", raw)
	}
	password, _ := parsed.User.Password()
	if parsed.User.Username() != "user" || password != "p@ss word" {
		t.Fatalf("kredensial URI = %s", parsed.User.String())
	}
	if parsed.Query().Get("authSource") != "admin" || parsed.Query().Get("tls") != "true" {
		t.Fatalf("query URI = %s", parsed.RawQuery)
	}
}
