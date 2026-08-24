package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLiveIndicatorContract(t *testing.T) {
	allowedLiveDots := map[string]int{
		"app/layout.gsx":        1,
		"app/page.gsx":          2,
		"app/matchups/page.gsx": 3,
		"app/draft/page.gsx":    1,
		"app/wire/page.gsx":     1,
	}
	seenLiveDots := make(map[string]int)
	staticLabels := map[string]bool{}
	if err := filepath.Walk("..", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if path == "../.git" || path == "../dist" || path == "../data" {
				return filepath.SkipDir
			}
			return nil
		}
		relative, relErr := filepath.Rel("..", path)
		if relErr != nil {
			return relErr
		}
		path = filepath.ToSlash(relative)
		if !strings.HasSuffix(path, ".gsx") && path != "public/styles.css" {
			return nil
		}
		contents, err := os.ReadFile(filepath.Join("..", path))
		if err != nil {
			return err
		}
		source := string(contents)
		if strings.Contains(source, "status-pin") {
			t.Errorf("%s retains unmapped status-pin decoration", path)
		}
		if strings.Contains(source, "access-light") {
			t.Errorf("%s retains obsolete access-light decoration", path)
		}
		for _, line := range strings.Split(source, "\n") {
			if strings.Contains(line, `class="signal-mark`) {
				staticLabels[path] = true
				if !strings.Contains(line, `aria-hidden="true"`) {
					t.Errorf("%s signal-mark is not aria-hidden: %s", path, strings.TrimSpace(line))
				}
			}
			if !strings.Contains(line, `class="live-dot`) {
				continue
			}
			seenLiveDots[path]++
			if _, ok := allowedLiveDots[path]; !ok {
				t.Errorf("%s contains an unapproved live-dot site: %s", path, strings.TrimSpace(line))
				continue
			}
			if (path == "app/matchups/page.gsx" || path == "app/draft/page.gsx" || path == "app/wire/page.gsx") && !strings.Contains(line, "live-dot--bound") {
				t.Errorf("%s live-dot must be bound to authoritative state: %s", path, strings.TrimSpace(line))
			}
			if !strings.Contains(line, `aria-hidden="true"`) {
				t.Errorf("%s live-dot is not aria-hidden: %s", path, strings.TrimSpace(line))
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for path, want := range allowedLiveDots {
		if got := seenLiveDots[path]; got != want {
			t.Errorf("%s live-dot sites = %d, want explicit allowlist count %d", path, got, want)
		}
	}
	if len(staticLabels) < 15 {
		t.Fatalf("static pages lost signal-mark coverage: found %d files", len(staticLabels))
	}
	css, err := os.ReadFile("../public/styles.css")
	if err != nil {
		t.Fatal(err)
	}
	stylesheet := string(css)
	for _, want := range []string{".signal-mark", ".signal-mark--cyan", ".signal-mark--hot", ".live-dot--bound:empty"} {
		if !strings.Contains(stylesheet, want) {
			t.Errorf("stylesheet missing indicator contract %q", want)
		}
	}
}
