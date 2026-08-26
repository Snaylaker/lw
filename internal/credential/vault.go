package credential

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	keyring "github.com/zalando/go-keyring"
)

const (
	// KeyringService and KeyringAccount are the non-secret coordinates shown to
	// the user after a successful save and usable in native keychain tools.
	KeyringService = "lw"
	KeyringAccount = "linear-api-key"
)

var (
	ErrNotFound           = errors.New("Linear API key not found")
	ErrKeyringUnavailable = errors.New("system keychain unavailable")
)

// Store is an explicit persistence destination. Choosing the file never retries
// the keychain: one user action always maps to one deterministic write.
type Store uint8

const (
	StoreKeyring Store = iota + 1
	StoreFile
)

// Location is the destination of a successful save. Constructors keep invalid
// combinations (for example, a keychain with a file path) out of the UI contract.
type Location struct {
	store Store
	path  string
}

func KeyringLocation() Location         { return Location{store: StoreKeyring} }
func FileLocation(path string) Location { return Location{store: StoreFile, path: path} }
func (l Location) Store() Store         { return l.store }
func (l Location) Path() string         { return l.path }

// Vault is the persistent credential boundary. Tests use an in-memory fake;
// production supports the system keychain and an explicitly selected owner-only
// file (needed on headless Linux).
type Vault interface {
	Load() (string, Source, error)
	Save(string, Store) (Location, error)
	Delete() error
}

type keyringBackend interface {
	Get(service, account string) (string, error)
	Set(service, account, secret string) error
	Delete(service, account string) error
}

type osKeyring struct{}

func (osKeyring) Get(service, account string) (string, error) {
	return keyring.Get(service, account)
}
func (osKeyring) Set(service, account, secret string) error {
	return keyring.Set(service, account, secret)
}
func (osKeyring) Delete(service, account string) error {
	return keyring.Delete(service, account)
}

type defaultVault struct {
	keyring keyringBackend
	path    string
}

// NewVault returns the normal persistent store. The fallback file lives beside
// config.json but separately from it, so normal preference rewrites never
// handle the secret.
func NewVault(configPath string) Vault {
	return &defaultVault{
		keyring: osKeyring{},
		path:    FallbackPath(configPath),
	}
}

func (v *defaultVault) Load() (string, Source, error) {
	if secret, err := v.keyring.Get(KeyringService, KeyringAccount); err == nil {
		if secret = strings.TrimSpace(secret); secret != "" {
			return secret, SourceKeyring, nil
		}
	}
	info, err := os.Stat(v.path)
	if err == nil && runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return "", "", fmt.Errorf("credential file permissions are %o, want 600", info.Mode().Perm())
	}
	payload, err := os.ReadFile(v.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", "", ErrNotFound
		}
		return "", "", fmt.Errorf("read credential file: %w", err)
	}
	secret := strings.TrimSpace(string(payload))
	if secret == "" {
		return "", "", ErrNotFound
	}
	return secret, SourceFile, nil
}

func (v *defaultVault) Save(secret string, target Store) (Location, error) {
	switch target {
	case StoreKeyring:
		if err := v.keyring.Set(KeyringService, KeyringAccount, secret); err != nil {
			// Native credential-store errors are deliberately discarded: some
			// backends include request details, and the UI only needs to know that
			// it may offer the explicitly approved file alternative.
			return Location{}, ErrKeyringUnavailable
		}
		if err := os.Remove(v.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return Location{}, err
		}
		return KeyringLocation(), nil
	case StoreFile:
		if err := writeSecretFile(v.path, []byte(secret+"\n")); err != nil {
			return Location{}, err
		}
		return FileLocation(v.path), nil
	default:
		return Location{}, errors.New("invalid credential store")
	}
}

func (v *defaultVault) Delete() error {
	var failures []error

	if err := v.keyring.Delete(KeyringService, KeyringAccount); err != nil {
		if !errors.Is(err, keyring.ErrNotFound) {
			failures = append(failures, fmt.Errorf("delete system keychain credential: %w", err))
		}
	} else if _, err := v.keyring.Get(KeyringService, KeyringAccount); !errors.Is(err, keyring.ErrNotFound) {
		if err == nil {
			err = errors.New("credential still exists")
		}
		failures = append(failures, fmt.Errorf("verify system keychain credential removal: %w", err))
	}

	if err := os.Remove(v.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		failures = append(failures, fmt.Errorf("delete credential file: %w", err))
	}
	return errors.Join(failures...)
}

// FallbackPath is shown before the user consents to owner-only file storage.
func FallbackPath(configPath string) string {
	return filepath.Join(filepath.Dir(configPath), "credentials")
}

func writeSecretFile(path string, payload []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".credentials-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(payload); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
