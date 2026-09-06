package screen

import (
	"context"
	"errors"

	"github.com/DituLin/Atrium/internal/domain"
)

// Validate checks a command payload against its kind (design §6.5). It
// normalises the payload in place so a `refresh` never carries stray fields
// into the database.
func (s *Service) Validate(ctx context.Context, kind domain.CommandKind, payload *domain.CommandPayload) error {
	switch kind {
	case domain.CommandNavigate:
		return s.validateNavigate(payload)
	case domain.CommandShow:
		return s.validateShow(ctx, payload)
	case domain.CommandRefresh:
		*payload = domain.CommandPayload{}
		return nil
	default:
		return domain.Errorf(domain.CodeInvalidCommand, "unknown command kind %q", kind)
	}
}

// validateNavigate accepts only the whitelisted local routes; a collection is
// optional and must itself be whitelisted (FR-14).
func (s *Service) validateNavigate(payload *domain.CommandPayload) error {
	if !payload.Route.NavigableRoute() {
		return domain.Errorf(domain.CodeInvalidCommand,
			"navigate accepts route dashboard or photos, got %q", payload.Route)
	}
	if payload.Collection != "" {
		if !domain.Collection(payload.Collection).Valid() {
			return domain.Errorf(domain.CodeInvalidCommand,
				"unknown collection %q", payload.Collection)
		}
		if payload.Route != domain.RoutePhotos {
			return domain.Errorf(domain.CodeInvalidCommand,
				"a collection is only meaningful with route photos")
		}
	}
	payload.PhotoID = ""
	return nil
}

// validateShow requires a photo a screen is allowed to see and whose preview
// already exists: the command is "display this now", not "start a job".
func (s *Service) validateShow(ctx context.Context, payload *domain.CommandPayload) error {
	if payload.PhotoID == "" {
		return domain.Errorf(domain.CodeInvalidCommand, "show requires photo_id")
	}
	photo, err := s.db.Photos().GetEligible(ctx, payload.PhotoID)
	if errors.Is(err, domain.ErrNotFound) {
		return domain.Errorf(domain.CodeInvalidCommand,
			"photo %q is not available to screens", payload.PhotoID)
	}
	if err != nil {
		return err
	}
	if photo.PreviewStatus != domain.PreviewReady {
		return domain.Errorf(domain.CodeInvalidCommand,
			"photo %q has no ready preview (%s)", payload.PhotoID, photo.PreviewStatus)
	}
	payload.Route = ""
	payload.Collection = ""
	return nil
}
