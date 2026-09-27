package shortcode

import (
	"crypto/rand"
	"math/big"
)

const (
	alphabet   = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	defaultLen = 7
)

func Generate() (string, error) {
	return GenerateN(defaultLen)
}

func GenerateN(n int) (string, error) {
	if n <= 0 {
		n = defaultLen
	}

	max := big.NewInt(int64(len(alphabet)))
	buf := make([]byte, n)
	for i := 0; i < n; i++ {
		num, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		buf[i] = alphabet[num.Int64()]
	}
	return string(buf), nil
}
