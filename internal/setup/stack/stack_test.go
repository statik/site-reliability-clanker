package stack

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestAWSCarriesEveryFileThatTofuNeeds compares the embedded files with the directory, so
// that a new .tf file cannot be left out of the binary.
func TestAWSCarriesEveryFileThatTofuNeeds(t *testing.T) {
	files, err := AWS()
	if err != nil {
		t.Fatal(err)
	}
	embedded := []string{}
	err = fs.WalkDir(files, ".", func(path string, entry fs.DirEntry, err error) error {
		if err == nil && !entry.IsDir() {
			embedded = append(embedded, path)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	tfFiles, err := filepath.Glob(filepath.Join("aws", "*.tf"))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{".terraform.lock.hcl", "keycloak-start.sh"}
	for _, path := range tfFiles {
		want = append(want, filepath.Base(path))
	}
	slices.Sort(want)
	slices.Sort(embedded)
	if !slices.Equal(embedded, want) {
		t.Fatalf("embedded files = %v, want %v", embedded, want)
	}
	for _, name := range []string{"main.tf", "versions.tf", "outputs.tf"} {
		if !slices.Contains(embedded, name) {
			t.Errorf("the stack lacks %s", name)
		}
	}
}

func TestAWSFilesHaveTheContentOfTheDirectory(t *testing.T) {
	files, err := AWS()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{".terraform.lock.hcl", "keycloak-start.sh", "versions.tf"} {
		embedded, err := fs.ReadFile(files, name)
		if err != nil {
			t.Fatal(err)
		}
		onDisk, err := os.ReadFile(filepath.Join("aws", name))
		if err != nil {
			t.Fatal(err)
		}
		if string(embedded) != string(onDisk) {
			t.Errorf("%s differs from the file in aws/", name)
		}
	}
	lock, err := fs.ReadFile(files, ".terraform.lock.hcl")
	if err != nil {
		t.Fatal(err)
	}
	for _, provider := range []string{"hashicorp/aws", "hashicorp/random", "hashicorp/tls"} {
		if !strings.Contains(string(lock), provider) {
			t.Errorf("the lock file has no entry for %s", provider)
		}
	}
}

func TestKeycloakRealmIsTheFileOfThePackage(t *testing.T) {
	onDisk, err := os.ReadFile("keycloak-realm.json")
	if err != nil {
		t.Fatal(err)
	}
	if KeycloakRealm() != string(onDisk) || !strings.Contains(KeycloakRealm(), `"realm"`) {
		t.Error("the embedded realm differs from keycloak-realm.json or has no realm name")
	}
}
