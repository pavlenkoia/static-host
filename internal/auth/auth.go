package auth

import "golang.org/x/crypto/bcrypt"

const AdminLogin = "admin"

func ValidPassword(password string) bool {
	return len(password) >= 12 && len(password) <= 72
}

func Hash(password string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	return string(b), err
}

func Compare(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}
