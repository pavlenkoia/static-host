package auth

import "testing"

func TestPasswordHash(t *testing.T) {
	for _, password := range []string{"", "short", string(make([]byte, 73))} {
		if ValidPassword(password) {
			t.Fatalf("accepted password length %d", len(password))
		}
	}
	for _, password := range []string{string(make([]byte, 12)), string(make([]byte, 72))} {
		if !ValidPassword(password) {
			t.Fatalf("rejected password length %d", len(password))
		}
	}
	password := "valid-password"
	if !ValidPassword(password) {
		t.Fatal("rejected valid password")
	}
	hash, err := Hash(password)
	if err != nil {
		t.Fatal(err)
	}
	if !Compare(hash, password) || Compare(hash, "different-password") {
		t.Fatal("password hash comparison failed")
	}
}
