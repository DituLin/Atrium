package httpapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/DituLin/Atrium/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestArchiveUploadContextCancellationIsNotInternalError(t *testing.T) {
	// net/http cancels the connection context on a failed body Read. BlobStore
	// checks that context before returning the network timeout, including when
	// Put joins it with staging cleanup errors.
	for _, input := range []error{context.Canceled, errors.Join(context.Canceled, nil), context.DeadlineExceeded} {
		e, ok := domain.AsError(archiveRequestError(input))
		require.True(t, ok)
		require.Equal(t, http.StatusRequestTimeout, e.HTTPStatus())
	}
}

func TestArchiveEnvelopeFirstClosingLineAcrossReadSplits(t *testing.T) {
	prefix := "preamble\n--boundaryX\n--boundary--\n--boundary\r\nHeader: value\r\n\r\n"
	markerLike := "embedded--boundary-- \r\n\n--boundary--\t\r\n\r\n--boundaryX\r\ncontent"
	for _, tc := range []struct {
		name, wire string
		valid      bool
	}{
		{"canonical", prefix + markerLike + "\r\n--boundary--\r\n", true},
		{"space", prefix + markerLike + "\r\n--boundary-- \r\n", true},
		{"tab", prefix + markerLike + "\r\n--boundary--\t\r\n", true},
		{"EOF", prefix + markerLike + "\r\n--boundary-- \t", true},
		{"space_epilogue", prefix + markerLike + "\r\n--boundary-- \r\nhidden\r\n--boundary--\r\n", false},
		{"tab_epilogue", prefix + markerLike + "\r\n--boundary--\t\r\nhidden", false},
		{"lf", "preamble\r\n--boundaryX\r\n--boundary \t\nHeader: value\n\ncontent\n--boundary--\t\n", true},
		{"lf_epilogue", "preamble\r\n--boundaryX\r\n--boundary \t\nHeader: value\n\ncontent\n--boundary--\t\nhidden\r\n--boundary--\r\n", false},
		{"unterminated", prefix + markerLike + "\r\n--boundary--\r", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for split := 0; split <= len(tc.wire); split++ {
				reader := io.MultiReader(strings.NewReader(tc.wire[:split]), strings.NewReader(tc.wire[split:]))
				envelope := &archiveEnvelopeReader{reader: reader, boundary: []byte("--boundary")}
				_, err := io.Copy(io.Discard, envelope)
				require.NoError(t, err)
				require.Equal(t, tc.valid, envelope.validEnd(), "split=%d", split)
				require.EqualValues(t, len(tc.wire), envelope.total)
			}
		})
	}
}

func TestArchiveEnvelopeLongNonBoundaryLines(t *testing.T) {
	// Long preamble/content lines are discarded by the scanner after their
	// prefix fails, rather than buffered or truncated into apparent delimiters.
	prefix := strings.Repeat("p", 1<<20) + "\n--boundary\r\nHeader: value\r\n\r\n"
	body := strings.Repeat("x", 1<<20) + "--boundary--\r\ncontent\r\n--boundary--\t\r\n"
	envelope := &archiveEnvelopeReader{reader: io.MultiReader(strings.NewReader(prefix), strings.NewReader(body)), boundary: []byte("--boundary")}
	_, err := io.Copy(io.Discard, envelope)
	require.NoError(t, err)
	require.True(t, envelope.validEnd())
	require.EqualValues(t, len(prefix)+len(body), envelope.total)
}
