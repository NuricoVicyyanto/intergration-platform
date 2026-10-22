package auth

import "testing"

func TestPasswordHashRoundTrip(t *testing.T) {
	hash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyPassword("correct horse battery staple", hash) {
		t.Fatal("password valid ditolak")
	}
	if VerifyPassword("password salah", hash) {
		t.Fatal("password salah diterima")
	}
}

func TestPasswordMinimumLength(t *testing.T) {
	if _, err := HashPassword("pendek"); err == nil {
		t.Fatal("password pendek seharusnya ditolak")
	}
}
