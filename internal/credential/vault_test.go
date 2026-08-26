package credential

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	keyring "github.com/zalando/go-keyring"
)

type fakeKeyring struct {
	secret    string
	getErr    error
	setErr    error
	setCalls  int
	deleteErr error
}

func (k *fakeKeyring) Get(string, string) (string, error) {
	if k.getErr != nil {
		return "", k.getErr
	}
	if k.secret == "" {
		return "", keyring.ErrNotFound
	}
	return k.secret, nil
}
func (k *fakeKeyring) Set(_, _, secret string) error {
	k.setCalls++
	if k.setErr != nil {
		return k.setErr
	}
	k.secret = secret
	return nil
}
func (k *fakeKeyring) Delete(string, string) error {
	if k.deleteErr != nil {
		return k.deleteErr
	}
	k.secret = ""
	return nil
}

func TestKeyringCoordinatesAreStableAndUserVisible(t *testing.T) {
	if KeyringService != "lw" || KeyringAccount != "linear-api-key" {
		t.Errorf("coordinates = %q, %q", KeyringService, KeyringAccount)
	}
}

func TestVaultUsesTheSystemKeyringWhenAvailable(t *testing.T) {
	backend := &fakeKeyring{}
	vault := &defaultVault{keyring: backend, path: filepath.Join(t.TempDir(), "credentials")}
	location, err := vault.Save("secret", StoreKeyring)
	if err != nil || location.Store() != StoreKeyring {
		t.Fatalf("save = %+v, %v", location, err)
	}
	key, source, err := vault.Load()
	if err != nil || key != "secret" || source != SourceKeyring {
		t.Fatalf("load = %q, %q, %v", key, source, err)
	}
}

func TestVaultRefusesAnOverexposedCredentialFile(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("Windows permissions are ACL-based")
	}
	path := filepath.Join(t.TempDir(), "credentials")
	if err := os.WriteFile(path, []byte("secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	vault := &defaultVault{keyring: &fakeKeyring{getErr: errors.New("no keychain")}, path: path}
	if _, _, err := vault.Load(); err == nil {
		t.Fatal("overexposed credential file was accepted")
	}
}

func TestVaultReportsAKeychainDeletionFailure(t *testing.T) {
	backend := &fakeKeyring{secret: "secret", deleteErr: errors.New("locked")}
	vault := &defaultVault{keyring: backend, path: filepath.Join(t.TempDir(), "credentials")}
	if err := vault.Delete(); err == nil {
		t.Fatal("keychain deletion failure was ignored")
	}
	if backend.secret != "secret" {
		t.Fatal("fake keychain unexpectedly removed the secret")
	}
}

func TestVaultStillRemovesTheFileWhenKeyringDeletionFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials")
	if err := os.WriteFile(path, []byte("file-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	backend := &fakeKeyring{secret: "keyring-secret", deleteErr: errors.New("locked")}
	vault := &defaultVault{keyring: backend, path: path}

	if err := vault.Delete(); err == nil {
		t.Fatal("partial deletion was reported as success")
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("credential file was not removed: %v", err)
	}
	if backend.secret != "keyring-secret" {
		t.Fatal("failed keyring deletion unexpectedly changed the credential")
	}
}

func TestVaultFallsBackToAnOwnerOnlyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config", "credentials")
	vault := &defaultVault{keyring: &fakeKeyring{setErr: errors.New("no keychain"), getErr: errors.New("no keychain")}, path: path}
	if _, err := vault.Save("secret", StoreKeyring); !errors.Is(err, ErrKeyringUnavailable) {
		t.Fatalf("keyring save = %v", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("credential file was written without consent")
	}
	location, err := vault.Save("secret", StoreFile)
	if err != nil || location.Store() != StoreFile || location.Path() != path {
		t.Fatalf("save = %+v, %v", location, err)
	}
	if calls := vault.keyring.(*fakeKeyring).setCalls; calls != 1 {
		t.Fatalf("keyring writes = %d, want only the initial attempt", calls)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %o", info.Mode().Perm())
	}
	key, source, err := vault.Load()
	if err != nil || key != "secret" || source != SourceFile {
		t.Fatalf("load = %q, %q, %v", key, source, err)
	}
	if err := vault.Delete(); err == nil {
		t.Fatal("logout claimed success without verifying the unavailable keychain")
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("credential file still exists: %v", err)
	}
}
