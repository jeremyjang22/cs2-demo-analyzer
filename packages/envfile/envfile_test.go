package envfile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseLine(t *testing.T) {
	for line, want := range map[string][2]string{
		"DATABASE_URL=postgres://u:p@h:5432/db?search_path=public&sslmode=require\r": {"DATABASE_URL", "postgres://u:p@h:5432/db?search_path=public&sslmode=require"},
		"export R2_BUCKET=cs2-demo-payloads":                                         {"R2_BUCKET", "cs2-demo-payloads"},
		`TITLE="a b"`:                                                                {"TITLE", "a b"},
		"EMPTY=":                                                                     {"EMPTY", ""},
		"WITH_EQUALS=a=b":                                                            {"WITH_EQUALS", "a=b"},
	} {
		k, v, ok := parseLine(line)
		if !ok || k != want[0] || v != want[1] {
			t.Errorf("%q: got %q=%q ok=%v", line, k, v, ok)
		}
	}
	for _, line := range []string{"", "  ", "# DATABASE_URL=x", "no equals", "TWO WORDS=x"} {
		if _, _, ok := parseLine(line); ok {
			t.Errorf("%q: want skipped", line)
		}
	}
}

func TestLoadKeepsWhatIsAlreadySet(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("ENVFILE_A=from file\r\nENVFILE_B=from file\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ENVFILE_A", "from the environment")
	t.Setenv("ENVFILE_B", "")
	os.Unsetenv("ENVFILE_B")

	if found, err := Load(path); !found || err != nil {
		t.Fatalf("Load = %v, %v", found, err)
	}
	if got := os.Getenv("ENVFILE_A"); got != "from the environment" {
		t.Errorf("A = %q; the environment should win", got)
	}
	if got := os.Getenv("ENVFILE_B"); got != "from file" {
		t.Errorf("B = %q", got)
	}
	if found, err := Load(filepath.Join(t.TempDir(), "missing")); found || err != nil {
		t.Errorf("missing file: %v, %v", found, err)
	}
}
