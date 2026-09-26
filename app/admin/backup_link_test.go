package admin

import (
	"strings"
	"testing"

	"golang.org/x/net/html"
)

func TestBackupLinkUsesBrowserDownload(t *testing.T) {
	tokens := html.NewTokenizer(strings.NewReader(renderAdminPage(t)))
	for tokens.Next() != html.ErrorToken {
		token := tokens.Token()
		if token.Type != html.StartTagToken || token.Data != "a" {
			continue
		}
		isBackup, download := false, false
		for _, attr := range token.Attr {
			isBackup = isBackup || attr.Key == "href" && attr.Val == "/admin/backup.tar.gz"
			download = download || attr.Key == "download"
		}
		if isBackup {
			if !download {
				t.Fatal("backup link needs download so managed navigation leaves the archive to the browser")
			}
			return
		}
	}
	t.Fatal("backup download link missing")
}
