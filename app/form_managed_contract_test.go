package app

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var postFormOpenTag = regexp.MustCompile("(?is)<form\\b[^>]*>")

// TestPostFormsDeclareManagedBehavior is the discovery guard for progressive
// action behavior. A plain POST form otherwise falls back to a full-page
// navigation and can silently lose managed validation/feedback. The known
// native identity routes are the only deliberate exceptions: both are
// explicitly marked unmanaged and remain outside the file-action pipeline.
func TestPostFormsDeclareManagedBehavior(t *testing.T) {
	err := filepath.WalkDir(".", func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || filepath.Ext(path) != ".gsx" {
			return nil
		}
		source, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, tag := range postFormOpenTag.FindAllString(string(source), -1) {
			if !postFormMethod(tag) || managedFormTag(tag) || intentionalNativeTeamForm(path, tag) {
				continue
			}
			t.Errorf("%s has an ordinary POST form without data-gosx-managed=\"true\": %s", path, strings.TrimSpace(tag))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func postFormMethod(tag string) bool {
	method := regexp.MustCompile("(?i)\\bmethod\\s*=\\s*[\"']post[\"']")
	return method.MatchString(tag)
}

func managedFormTag(tag string) bool {
	managed := regexp.MustCompile("(?i)\\bdata-gosx-managed(?:\\s*=\\s*(?:\"true\"|'true'))?(?:\\s|>)")
	return managed.MatchString(tag)
}

func intentionalNativeTeamForm(path, tag string) bool {
	switch filepath.ToSlash(path) {
	case "admin/page.gsx", "team/page.gsx":
	default:
		return false
	}
	if !regexp.MustCompile("(?i)\\bdata-gosx-managed\\s*=\\s*[\"']false[\"']").MatchString(tag) {
		return false
	}
	action := regexp.MustCompile("(?i)\\baction\\s*=\\s*[\"']([^\"']+)[\"']").FindStringSubmatch(tag)
	if len(action) != 2 {
		return false
	}
	switch action[1] {
	case "/avatar/upload":
		return regexp.MustCompile("(?i)\\benctype\\s*=\\s*[\"']multipart/form-data[\"']").MatchString(tag)
	case "/avatar/badge":
		return true
	default:
		return false
	}
}
