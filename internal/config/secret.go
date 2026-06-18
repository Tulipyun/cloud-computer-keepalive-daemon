package config

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"os/user"
	"runtime"

	"golang.org/x/crypto/scrypt"
)

const secretBoxScheme = "machine-aes-gcm-v1"

type SecretBox struct {
	Scheme string `json:"scheme"`
	Salt   string `json:"salt"`
	Nonce  string `json:"nonce"`
	Data   string `json:"data"`
}

func EncryptLocalSecret(plain string) (*SecretBox, error) {
	salt := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return nil, err
	}
	key, err := localSecretKey(salt)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	ciphertext := gcm.Seal(nil, nonce, []byte(plain), nil)
	return &SecretBox{
		Scheme: secretBoxScheme,
		Salt:   base64.StdEncoding.EncodeToString(salt),
		Nonce:  base64.StdEncoding.EncodeToString(nonce),
		Data:   base64.StdEncoding.EncodeToString(ciphertext),
	}, nil
}

func DecryptLocalSecret(box *SecretBox) (string, error) {
	if box == nil {
		return "", fmt.Errorf("missing encrypted secret")
	}
	if box.Scheme != secretBoxScheme {
		return "", fmt.Errorf("unsupported secret scheme %q", box.Scheme)
	}
	salt, err := base64.StdEncoding.DecodeString(box.Salt)
	if err != nil {
		return "", err
	}
	nonce, err := base64.StdEncoding.DecodeString(box.Nonce)
	if err != nil {
		return "", err
	}
	ciphertext, err := base64.StdEncoding.DecodeString(box.Data)
	if err != nil {
		return "", err
	}
	key, err := localSecretKey(salt)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	plain, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

func localSecretKey(salt []byte) ([]byte, error) {
	hostname, _ := os.Hostname()
	username := os.Getenv("USERNAME")
	if username == "" {
		username = os.Getenv("USER")
	}
	if current, err := user.Current(); err == nil && current.Username != "" {
		username = current.Username
	}
	material := "cloud-computer-keepalive-local-daemon\x00" + runtime.GOOS + "\x00" + hostname + "\x00" + username
	return scrypt.Key([]byte(material), salt, 32768, 8, 1, 32)
}
