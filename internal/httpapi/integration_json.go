package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/DituLin/Atrium/internal/domain"
)

func decodeIntegrationJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(dst); err != nil {
		return domain.Errorf(domain.CodeInvalidRequest, "invalid integration request")
	}
	var extra any
	if !errors.Is(d.Decode(&extra), io.EOF) {
		return domain.Errorf(domain.CodeInvalidRequest, "expected exactly one JSON value")
	}
	return nil
}
