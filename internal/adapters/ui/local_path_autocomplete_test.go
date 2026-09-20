package ui

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestLocalPathSuggestions(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"app file.txt", "中文[文件].txt", ".hidden"} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "app directory"), 0o700); err != nil {
		t.Fatal(err)
	}
	separator := string(os.PathSeparator)
	got := localPathSuggestions(filepath.Join(dir, "app"))
	want := []string{
		filepath.Join(dir, "app directory") + separator,
		filepath.Join(dir, "app file.txt"),
	}
	if !slices.Equal(got, want) {
		t.Fatalf("suggestions = %q, want %q", got, want)
	}
	if got := localPathSuggestions(dir + separator); slices.Contains(got, filepath.Join(dir, ".hidden")) {
		t.Fatalf("hidden file appeared without a dot prefix: %q", got)
	}
	if got := localPathSuggestions(dir + separator + "."); !slices.Contains(got, filepath.Join(dir, ".hidden")) {
		t.Fatalf("hidden file missing for dot prefix: %q", got)
	}
	if got := localPathSuggestions(filepath.Join(dir, "中文")); !slices.Contains(got, filepath.Join(dir, "中文[文件].txt")) {
		t.Fatalf("Chinese filename missing: %q", got)
	}
}

func TestLocalPathSuggestionsTildeAndMissingDirectory(t *testing.T) {
	if got := localPathSuggestions("~"); !slices.Equal(got, []string{"~" + string(os.PathSeparator)}) {
		t.Fatalf("tilde suggestion = %q", got)
	}
	if got := localPathSuggestions("~/this-directory-should-not-exist-lazyssh-123/"); len(got) != 0 {
		t.Fatalf("missing directory suggestions = %q", got)
	}
	if got := localPathSuggestions(""); len(got) != 0 {
		t.Fatalf("empty input suggestions = %q", got)
	}
}
