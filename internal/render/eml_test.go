package render

import (
	"io"
	"mime"
	"mime/multipart"
	"net/mail"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// loadEmlHTML extracts the text/html part from an RFC 5322 message
// stored on disk. Used by the table-classifier corpus tests in
// htmltable_test.go and reusable for any future testdata category.
//
// Walks single-part bodies directly and multipart/* bodies via
// mime/multipart. Accepts testing.TB so benchmarks (testing.B) can
// load the same fixtures as unit tests.
func loadEmlHTML(tb testing.TB, path string) string {
	tb.Helper()
	f, err := os.Open(path)
	require.NoErrorf(tb, err, "open %s", path)
	defer f.Close()
	msg, err := mail.ReadMessage(f)
	require.NoErrorf(tb, err, "parse %s", path)
	ct := msg.Header.Get("Content-Type")
	mediaType, params, err := mime.ParseMediaType(ct)
	require.NoErrorf(tb, err, "media type %q in %s", ct, path)
	if strings.HasPrefix(mediaType, "multipart/") {
		mr := multipart.NewReader(msg.Body, params["boundary"])
		for {
			part, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			require.NoError(tb, err)
			pmt, _, perr := mime.ParseMediaType(part.Header.Get("Content-Type"))
			if perr == nil && pmt == "text/html" {
				body, rerr := io.ReadAll(part)
				require.NoError(tb, rerr)
				return string(body)
			}
		}
		tb.Fatalf("no text/html part in %s", filepath.Base(path))
	}
	require.Equalf(tb, "text/html", mediaType, "%s is not text/html", filepath.Base(path))
	body, err := io.ReadAll(msg.Body)
	require.NoError(tb, err)
	return string(body)
}
