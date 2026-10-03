package httpapi

import (
	"context"
	"errors"
	"net/http"
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
