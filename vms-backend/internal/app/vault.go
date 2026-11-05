package app

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
)

func loadVault(dir string, allowCreate bool) (cipher.AEAD, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "camera-key.bin")
	key, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		if !allowCreate {
			return nil, errors.New("camera encryption key missing: restore the installation key alongside its database")
		}
		key = make([]byte, 32)
		if _, err = rand.Read(key); err != nil {
			return nil, err
		}
		f, e := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if errors.Is(e, os.ErrExist) {
			key, err = os.ReadFile(path)
		} else if e != nil {
			return nil, e
		} else {
			_, err = f.Write(key)
			if err == nil {
				err = f.Sync()
			}
			closeErr := f.Close()
			if err == nil {
				err = closeErr
			}
		}
	}
	if err != nil {
		return nil, err
	}
	if len(key) != 32 {
		return nil, errors.New("invalid camera encryption key")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
func (a *App) seal(secret []byte, origin string) ([]byte, error) {
	if a.vault == nil {
		return nil, errors.New("camera vault unavailable")
	}
	nonce := make([]byte, a.vault.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return a.vault.Seal(nonce, nonce, secret, []byte(origin)), nil
}
func (a *App) unseal(secret []byte, origin string) ([]byte, error) {
	if a.vault == nil || len(secret) < a.vault.NonceSize() {
		return nil, errors.New("camera vault unavailable")
	}
	n := a.vault.NonceSize()
	return a.vault.Open(nil, secret[:n], secret[n:], []byte(origin))
}
